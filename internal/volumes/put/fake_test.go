package put

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/volumes/keypair"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

const (
	fakeSessionTTL = 24 * time.Hour
	fakeTokenTTL   = time.Hour
	fakeAckPrefix  = "ack"
)

// States of a session in the fake.
const (
	fakeOpen      = "open"
	fakeCompleted = "completed"
	fakeAborted   = "aborted"
)

type vectorFile struct {
	KeyPair   vectorKeyPair   `json:"keyPair"`
	Statement vectorStatement `json:"statement"`
}

type vectorKeyPair struct {
	Organization string `json:"organization"`
	KeyPairID    string `json:"keyPairId"`
	PrivateKey   string `json:"privateKey"`
	PublicKey    string `json:"publicKey"`
}

type vectorStatement struct {
	Encoded   string `json:"encoded"`
	Signature string `json:"signature"`
}

// vectorKeys is the format vectors' organization, key pair and signed key-pair statement.
type vectorKeys struct {
	org        [16]byte
	keyPair    [16]byte
	privateKey []byte
	publicKey  []byte
	statement  []byte
	signature  []byte
}

func loadVectorKeys(t *testing.T) vectorKeys {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "volumeformat", "testdata", "format-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw vectorFile
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return vectorKeys{
		org:        [16]byte(uuid.MustParse(raw.KeyPair.Organization)),
		keyPair:    [16]byte(uuid.MustParse(raw.KeyPair.KeyPairID)),
		privateKey: unhex(t, raw.KeyPair.PrivateKey),
		publicKey:  unhex(t, raw.KeyPair.PublicKey),
		statement:  unhex(t, raw.Statement.Encoded),
		signature:  unhex(t, raw.Statement.Signature),
	}
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeKeys stands in for the key-pair package: it checks the vectors' statement and seals as keypair.Session does.
type fakeKeys struct {
	vec      vectorKeys
	refuse   error
	mu       sync.Mutex
	opens    int
	sessions []*fakeSession
}

func (k *fakeKeys) Open(statement, signature []byte, organization [16]byte) (KeySession, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.opens++
	if k.refuse != nil {
		return nil, k.refuse
	}
	if !bytes.Equal(statement, k.vec.statement) || !bytes.Equal(signature, k.vec.signature) {
		return nil, fmt.Errorf("%w: signature does not verify", keypair.ErrStatement)
	}
	if organization != k.vec.org {
		return nil, fmt.Errorf("%w: organization is not the configured one", keypair.ErrStatement)
	}
	sealer, err := volumeformat.Encapsulate(k.vec.publicKey)
	if err != nil {
		return nil, err
	}
	s := &fakeSession{
		vec:    k.vec,
		sealer: sealer,
	}
	k.sessions = append(k.sessions, s)
	return s, nil
}

func (k *fakeKeys) sealed() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, s := range k.sessions {
		n += len(s.fileKeys())
	}
	return n
}

type fakeSession struct {
	vec      vectorKeys
	sealer   *volumeformat.SessionSealer
	session  [16]byte
	key      []byte
	original []byte
	mu       sync.Mutex
	files    []*fakeFileKey
	cleared  atomic.Bool
}

func (s *fakeSession) KeyPairID() [16]byte { return s.vec.keyPair }

func (s *fakeSession) Encapsulation() []byte { return s.sealer.Encapsulation() }

func (s *fakeSession) Bind(session [16]byte) error {
	key, err := s.sealer.SessionKey(s.vec.org, s.vec.keyPair, session)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session, s.key = session, key
	s.original = slices.Clone(key)
	return nil
}

func (s *fakeSession) Key() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key
}

func (s *fakeSession) SealFile(digest [32]byte, size uint64) (FileKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := volumeformat.FileIdentity{
		Organization: s.vec.org,
		SHA256:       digest,
		Size:         size,
		Attempt:      volumeformat.NewAttemptID(),
	}
	dataKey := volumeformat.NewDataKey()
	sealed, err := volumeformat.WrapDataKey(s.key, s.session, id, dataKey)
	if err != nil {
		return nil, err
	}
	fk := &fakeFileKey{
		dataKey:  dataKey,
		original: slices.Clone(dataKey),
		sealed:   sealed,
		identity: id,
	}
	s.files = append(s.files, fk)
	return fk, nil
}

func (s *fakeSession) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.key)
	s.cleared.Store(true)
}

func (s *fakeSession) fileKeys() []*fakeFileKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.files)
}

type fakeFileKey struct {
	dataKey  []byte
	original []byte
	sealed   []byte
	identity volumeformat.FileIdentity
	cleared  atomic.Bool
}

// keyMaterial returns copies of every key the fake handed out, as they were before clearing.
func (k *fakeKeys) keyMaterial() [][]byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out [][]byte
	for _, s := range k.sessions {
		out = append(out, slices.Clone(s.original))
		for _, fk := range s.fileKeys() {
			out = append(out, slices.Clone(fk.original), slices.Clone(fk.sealed))
		}
	}
	return out
}

// allCleared reports whether every session and file key was cleared.
func (k *fakeKeys) allCleared() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, s := range k.sessions {
		if !s.cleared.Load() {
			return false
		}
		for _, fk := range s.fileKeys() {
			if !fk.cleared.Load() {
				return false
			}
		}
	}
	return true
}

// clearedFiles reports, for the first session's file keys in sealing order, which are cleared.
func (k *fakeKeys) clearedFiles() []bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []bool
	for _, fk := range k.sessions[0].fileKeys() {
		out = append(out, fk.cleared.Load())
	}
	return out
}

func (f *fakeFileKey) DataKey() []byte                     { return f.dataKey }
func (f *fakeFileKey) Attempt() [16]byte                   { return f.identity.Attempt }
func (f *fakeFileKey) Sealed() []byte                      { return f.sealed }
func (f *fakeFileKey) Identity() volumeformat.FileIdentity { return f.identity }

func (f *fakeFileKey) Clear() {
	clear(f.dataKey)
	f.cleared.Store(true)
}

type fakeEntry struct {
	Mode   uint32
	Size   int64
	SHA256 string
	BlobID string
}

type fakeVolume struct {
	id    string
	files map[string]fakeEntry
}

type blobKey struct {
	sha  string
	size int64
}

type storedPart struct {
	ciphertext []byte
	ack        string
}

type fakeFile struct {
	id     string
	upload *fakeUpload
	decl   FileDeclaration
	state  string
	layout volumeformat.Layout
	parts  map[int]storedPart
	answer DeclarationAnswer
}

type fakeUpload struct {
	id        string
	vol       *fakeVolume
	enc       []byte
	state     string
	expiresAt time.Time
	files     map[string]*fakeFile
	paths     map[string]string
	order     []string
	result    UploadResult
}

// partRequest is a request to the fake upload endpoint.
type partRequest struct {
	FileID  string
	Path    string
	Part    int
	Attempt int
}

type fakeFault struct {
	Status int
	Slug   string
}

type putRecord struct {
	FileID string
	Path   string
	Part   int
	Status int
	Salt   []byte
}

// fakeCloud is the in-memory Volumes API and upload endpoint for one organization.
type fakeCloud struct {
	t      *testing.T
	vec    vectorKeys
	server *httptest.Server

	mu      sync.Mutex
	volumes map[string]*fakeVolume
	blobs   map[blobKey]string
	uploads map[string]*fakeUpload
	last    *fakeUpload
	files   map[string]*fakeFile
	calls   []string
	puts    []putRecord
	renewed []string

	firstTargets   int
	declareTTL     time.Duration
	renewTTL       time.Duration
	sessionTTL     time.Duration
	notReady       int
	maxFileBytes   int64
	fault          func(partRequest) *fakeFault
	onPut          func(partRequest)
	onDeclare      func(FileDeclarations)
	onOpen         func()
	onCompleteFile func(path string)
	statementBytes *KeyPairStatement
	tokens         []string
	acks           []string
}

// issued returns every part token and acknowledgement the cloud handed out.
func (c *fakeCloud) issued() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append(slices.Clone(c.tokens), c.acks...)
}

func newFakeCloud(t *testing.T, vec vectorKeys) *fakeCloud {
	t.Helper()
	c := &fakeCloud{
		t:            t,
		vec:          vec,
		volumes:      map[string]*fakeVolume{},
		blobs:        map[blobKey]string{},
		uploads:      map[string]*fakeUpload{},
		files:        map[string]*fakeFile{},
		firstTargets: 16,
		declareTTL:   fakeTokenTTL,
		renewTTL:     fakeTokenTTL,
		sessionTTL:   fakeSessionTTL,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/parts", c.servePart)
	c.server = httptest.NewTLSServer(mux)
	t.Cleanup(c.server.Close)
	return c
}

func (c *fakeCloud) newVolume(name, env string) VolumeRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	ref := VolumeRef{
		Name:        name,
		Environment: env,
	}
	c.volumes[volumeKey(ref)] = &fakeVolume{
		id:    uuid.NewString(),
		files: map[string]fakeEntry{},
	}
	return ref
}

func volumeKey(v VolumeRef) string { return v.Environment + "/" + v.Name }

func (c *fakeCloud) logCall(name string) { c.calls = append(c.calls, name) }

func (c *fakeCloud) callLog() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

func (c *fakeCloud) count(name string) int {
	n := 0
	for _, call := range c.callLog() {
		if call == name {
			n++
		}
	}
	return n
}

func (c *fakeCloud) putLog() []putRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.puts)
}

func (c *fakeCloud) volume(ref VolumeRef) (*fakeVolume, *ProblemError) {
	v, ok := c.volumes[volumeKey(ref)]
	if !ok {
		return nil, &ProblemError{
			Status: http.StatusNotFound,
			Slug:   "volume-not-found",
			Detail: "the Volume does not exist",
		}
	}
	return v, nil
}

func (c *fakeCloud) upload(ref UploadRef) (*fakeUpload, *ProblemError) {
	up, ok := c.uploads[ref.ID]
	if !ok {
		return nil, &ProblemError{
			Status: http.StatusNotFound,
			Slug:   "volume-not-found",
			Detail: "the upload does not exist",
		}
	}
	return up, nil
}

func (c *fakeCloud) openUpload(ref UploadRef) (*fakeUpload, *ProblemError) {
	up, p := c.upload(ref)
	if p != nil {
		return nil, p
	}
	if up.state != fakeOpen {
		return nil, &ProblemError{
			Status: http.StatusConflict,
			Slug:   slugUploadNotOpen,
			Detail: "the upload session is not open",
		}
	}
	return up, nil
}

func (c *fakeCloud) LatestFiles(ctx context.Context, vol VolumeRef) (FileList, error) {
	if err := ctx.Err(); err != nil {
		return FileList{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("list")
	v, p := c.volume(vol)
	if p != nil {
		return FileList{}, p
	}
	return FileList{
		ManifestSHA256: c.digest(v),
		Files:          c.list(v),
	}, nil
}

func (c *fakeCloud) list(v *fakeVolume) []VolumeFile {
	files := make([]VolumeFile, 0, len(v.files))
	for path, e := range v.files {
		files = append(files, VolumeFile{
			Path:   path,
			Mode:   e.Mode,
			Size:   e.Size,
			SHA256: e.SHA256,
		})
	}
	slices.SortFunc(files, func(a, b VolumeFile) int { return strings.Compare(a.Path, b.Path) })
	return files
}

type manifestFile struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	BlobID string `json:"blobId"`
}

type manifest struct {
	FormatVersion    int            `json:"formatVersion"`
	OrganizationUUID string         `json:"organizationUuid"`
	VolumeID         string         `json:"volumeId"`
	Files            []manifestFile `json:"files"`
}

// manifestBytes is the Volume's file list as the publish output writes it.
func (c *fakeCloud) manifestBytes(v *fakeVolume) []byte {
	m := manifest{
		FormatVersion:    1,
		OrganizationUUID: uuid.UUID(c.vec.org).String(),
		VolumeID:         v.id,
		Files:            make([]manifestFile, 0, len(v.files)),
	}
	for path, e := range v.files {
		m.Files = append(m.Files, manifestFile{
			Path:   path,
			Mode:   e.Mode,
			Size:   e.Size,
			SHA256: e.SHA256,
			BlobID: e.BlobID,
		})
	}
	slices.SortFunc(m.Files, func(a, b manifestFile) int { return strings.Compare(a.Path, b.Path) })
	out, err := json.Marshal(m)
	if err != nil {
		c.t.Fatal(err)
	}
	return out
}

func (c *fakeCloud) digest(v *fakeVolume) string {
	if len(v.files) == 0 {
		return ""
	}
	sum := sha256.Sum256(c.manifestBytes(v))
	return hex.EncodeToString(sum[:])
}

// latest returns the Volume's latest files and their digest.
func (c *fakeCloud) latest(ref VolumeRef) ([]VolumeFile, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, p := c.volume(ref)
	if p != nil {
		c.t.Fatalf("latest: %v", p)
	}
	return c.list(v), c.digest(v)
}

func (c *fakeCloud) KeyPairStatement(ctx context.Context) (KeyPairStatement, error) {
	if err := ctx.Err(); err != nil {
		return KeyPairStatement{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("keypair")
	if c.notReady > 0 {
		c.notReady--
		return KeyPairStatement{}, &ProblemError{
			Status: http.StatusConflict,
			Slug:   slugKeyPairNotReady,
			Detail: "not ready",
		}
	}
	if c.statementBytes != nil {
		return *c.statementBytes, nil
	}
	return KeyPairStatement{
		Statement: base64.RawURLEncoding.EncodeToString(c.vec.statement),
		Signature: base64.RawURLEncoding.EncodeToString(c.vec.signature),
	}, nil
}

func (c *fakeCloud) OpenUpload(ctx context.Context, vol VolumeRef, open UploadOpen) (Upload, error) {
	if err := ctx.Err(); err != nil {
		return Upload{}, err
	}
	if c.onOpen != nil {
		c.onOpen()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("open")
	v, p := c.volume(vol)
	if p != nil {
		return Upload{}, p
	}
	if open.KeyPairID != uuid.UUID(c.vec.keyPair).String() {
		return Upload{}, &ProblemError{
			Status: http.StatusConflict,
			Slug:   "volume-key-pair-unknown",
			Detail: "unknown key pair",
		}
	}
	enc, err := base64.RawURLEncoding.DecodeString(open.Encapsulation)
	if err != nil || len(enc) != volumeformat.EncapsulationBytes {
		return Upload{}, &ProblemError{
			Status: http.StatusUnprocessableEntity,
			Detail: "bad encapsulation",
		}
	}
	now := time.Now()
	up := &fakeUpload{
		id:        uuid.NewString(),
		vol:       v,
		enc:       enc,
		state:     fakeOpen,
		expiresAt: now.Add(c.sessionTTL),
		files:     map[string]*fakeFile{},
		paths:     map[string]string{},
	}
	c.uploads[up.id] = up
	c.last = up
	return Upload{
		ID:        up.id,
		VolumeID:  v.id,
		State:     up.state,
		CreatedAt: now,
		ExpiresAt: up.expiresAt,
	}, nil
}

func (c *fakeCloud) token(f *fakeFile, part int, ttl time.Duration, expiresAt time.Time) Target {
	exp := time.Now().Add(ttl)
	if exp.After(expiresAt) {
		exp = expiresAt
	}
	length, err := f.layout.PartLength(int64(part))
	if err != nil {
		c.t.Fatal(err)
	}
	token := fmt.Sprintf("tok.%s.%d.%d.%d", f.id, part, length, exp.UnixMilli())
	c.tokens = append(c.tokens, token)
	return Target{
		PartNumber: part,
		URL:        c.server.URL + "/v1/parts",
		Token:      Secret(token),
		ExpiresAt:  exp,
	}
}

func (c *fakeCloud) DeclareFiles(ctx context.Context, ref UploadRef, batch FileDeclarations) (DeclarationResult, error) {
	if err := ctx.Err(); err != nil {
		return DeclarationResult{}, err
	}
	c.mu.Lock()
	hook := c.onDeclare
	c.mu.Unlock()
	if hook != nil {
		hook(batch)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("declare")
	up, p := c.openUpload(ref)
	if p != nil {
		return DeclarationResult{}, p
	}
	for i := range batch.Files {
		d := &batch.Files[i]
		pointer := fmt.Sprintf("/files/%d/", i)
		if len(d.SealedKey) != 80 || len(d.AttemptID) != 32 || len(d.SHA256) != 64 {
			return DeclarationResult{}, &ProblemError{
				Status:  http.StatusUnprocessableEntity,
				Detail:  "bad declaration",
				Pointer: pointer + "sealedKey",
			}
		}
		if id, dup := up.paths[d.Path]; dup && up.files[id].decl != *d {
			return DeclarationResult{}, &ProblemError{
				Status:  http.StatusConflict,
				Slug:    "volume-upload-path-declared",
				Detail:  "declared twice",
				Pointer: pointer + "path",
			}
		}
		if c.maxFileBytes > 0 && d.Size > c.maxFileBytes {
			return DeclarationResult{}, &ProblemError{
				Status:  http.StatusConflict,
				Slug:    slugLimitExceeded,
				Detail:  "maxFileBytes exceeds its limit",
				Limit:   limitFileBytes,
				Cap:     c.maxFileBytes,
				Pointer: pointer + "size",
			}
		}
	}
	var res DeclarationResult
	for i := range batch.Files {
		d := batch.Files[i]
		if id, dup := up.paths[d.Path]; dup {
			res.Files = append(res.Files, up.files[id].answer)
			continue
		}
		f := &fakeFile{
			id:     uuid.NewString(),
			upload: up,
			decl:   d,
			parts:  map[int]storedPart{},
		}
		file := UploadFile{
			ID:     f.id,
			Path:   d.Path,
			Mode:   d.Mode,
			Size:   d.Size,
			SHA256: d.SHA256,
		}
		if _, ok := c.blobs[blobKey{
			sha:  d.SHA256,
			size: d.Size,
		}]; ok {
			f.state = stateReused
			file.State = stateReused
			res.Files = append(res.Files, DeclarationAnswer{
				File: file,
				Blob: &BlobReference{
					SHA256: d.SHA256,
					Size:   d.Size,
				},
			})
		} else {
			layout, err := volumeformat.PlanLayout(d.Size)
			if err != nil {
				c.t.Fatal(err)
			}
			f.state = stateUploading
			f.layout = layout
			file.State = stateUploading
			file.AttemptID = d.AttemptID
			file.Layout = &PartLayout{
				SegmentBytes:          volumeformat.SegmentBytes,
				SegmentPlaintextBytes: volumeformat.SegmentPlaintextBytes,
				SegmentsPerPart:       layout.SegmentsPerPart,
				PartSizeBytes:         layout.PartSize,
				PartCount:             layout.PartCount,
				CiphertextBytes:       layout.CiphertextSize,
			}
			var targets []Target
			for part := 1; part <= min(c.firstTargets, int(layout.PartCount)); part++ {
				targets = append(targets, c.token(f, part, c.declareTTL, up.expiresAt))
			}
			res.Files = append(res.Files, DeclarationAnswer{
				File:          file,
				UploadTargets: targets,
			})
		}
		f.answer = res.Files[len(res.Files)-1]
		up.files[f.id] = f
		up.paths[d.Path] = f.id
		up.order = append(up.order, f.id)
		c.files[f.id] = f
	}
	return res, nil
}

func (c *fakeCloud) RenewParts(ctx context.Context, ref UploadRef, fileID string, parts []int) ([]Target, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("renew")
	up, p := c.openUpload(ref)
	if p != nil {
		return nil, p
	}
	f, ok := up.files[fileID]
	if !ok || f.state != stateUploading {
		return nil, &ProblemError{
			Status: http.StatusConflict,
			Slug:   "volume-upload-file-not-uploading",
			Detail: "the file is not uploading",
		}
	}
	targets := make([]Target, 0, len(parts))
	for _, part := range parts {
		if part < 1 || int64(part) > f.layout.PartCount {
			return nil, &ProblemError{
				Status: http.StatusUnprocessableEntity,
				Detail: "part beyond the layout",
			}
		}
		targets = append(targets, c.token(f, part, c.renewTTL, up.expiresAt))
	}
	c.renewed = append(c.renewed, fmt.Sprintf("%s:%v", f.decl.Path, parts))
	return targets, nil
}

func (c *fakeCloud) renewals() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.renewed)
}

func (c *fakeCloud) CompleteFile(ctx context.Context, ref UploadRef, fileID string, done FileCompletion) (UploadFile, error) {
	if err := ctx.Err(); err != nil {
		return UploadFile{}, err
	}
	if c.onCompleteFile != nil {
		c.onCompleteFile(c.pathOf(fileID))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("complete-file")
	up, p := c.openUpload(ref)
	if p != nil {
		return UploadFile{}, p
	}
	f, ok := up.files[fileID]
	if ok && f.state == stateVerified {
		return f.verifiedResult(), nil
	}
	if !ok || f.state != stateUploading {
		return UploadFile{}, &ProblemError{
			Status: http.StatusConflict,
			Slug:   "volume-upload-file-not-uploading",
			Detail: "the file is not uploading",
		}
	}
	if int64(len(done.Parts)) != f.layout.PartCount {
		return UploadFile{}, &ProblemError{
			Status: http.StatusConflict,
			Slug:   "volume-upload-parts-invalid",
			Detail: "a part is missing",
		}
	}
	var object []byte
	for i := range done.Parts {
		want := i + 1
		sp, stored := f.parts[want]
		if done.Parts[i].PartNumber != want || !stored || string(done.Parts[i].Ack) != sp.ack {
			return UploadFile{}, &ProblemError{
				Status: http.StatusConflict,
				Slug:   "volume-upload-parts-invalid",
				Detail: fmt.Sprintf("part %d has no valid acknowledgement", want),
			}
		}
		object = append(object, sp.ciphertext...)
	}
	crc := volumeformat.NewCRC64NVME()
	_, _ = crc.Write(object)
	if got := fmt.Sprintf("%016x", crc.Sum64()); got != done.CRC64NVME {
		f.state = "failed"
		return UploadFile{}, &ProblemError{
			Status: http.StatusConflict,
			Slug:   "volume-upload-crc-mismatch",
			Detail: "the stored CRC64/NVME differs from the submitted one",
		}
	}
	if err := c.decryptsTo(up, f, object); err != nil {
		f.state = "failed"
		return UploadFile{}, &ProblemError{
			Status: http.StatusConflict,
			Slug:   "volume-upload-crc-mismatch",
			Detail: err.Error(),
		}
	}
	f.state = stateVerified
	c.blobs[blobKey{
		sha:  f.decl.SHA256,
		size: f.decl.Size,
	}] = blobID(f.decl.SHA256, f.decl.Size)
	return f.verifiedResult(), nil
}

func (f *fakeFile) verifiedResult() UploadFile {
	return UploadFile{
		ID:     f.id,
		Path:   f.decl.Path,
		Mode:   f.decl.Mode,
		Size:   f.decl.Size,
		SHA256: f.decl.SHA256,
		State:  stateVerified,
	}
}

// decryptsTo opens the sealed key and the object as a reader of the Volume would and checks the plaintext.
func (c *fakeCloud) decryptsTo(up *fakeUpload, f *fakeFile, object []byte) error {
	session := [16]byte(uuid.MustParse(up.id))
	sessionKey, err := volumeformat.OpenSessionKey(c.vec.privateKey, up.enc, c.vec.org, c.vec.keyPair, session)
	if err != nil {
		return err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(f.decl.SealedKey)
	if err != nil {
		return err
	}
	attempt, err := hex.DecodeString(f.decl.AttemptID)
	if err != nil {
		return err
	}
	sum, err := hex.DecodeString(f.decl.SHA256)
	if err != nil {
		return err
	}
	id := volumeformat.FileIdentity{
		Organization: c.vec.org,
		SHA256:       [32]byte(sum),
		Size:         uint64(f.decl.Size),
		Attempt:      [16]byte(attempt),
	}
	dataKey, err := volumeformat.UnwrapDataKey(sessionKey, session, id, sealed)
	if err != nil {
		return fmt.Errorf("unwrap the data key: %w", err)
	}
	h := sha256.New()
	var size int64
	for part := int64(1); part <= f.layout.PartCount; part++ {
		offset := (part - 1) * f.layout.PartSize
		length, err := f.layout.PartLength(part)
		if err != nil {
			return err
		}
		plain, err := volumeformat.OpenPart(dataKey, id, f.layout, part, object[offset:offset+length])
		if err != nil {
			return fmt.Errorf("open part %d: %w", part, err)
		}
		h.Write(plain)
		size += int64(len(plain))
	}
	if size != f.decl.Size || hex.EncodeToString(h.Sum(nil)) != f.decl.SHA256 {
		return fmt.Errorf("the parts open to other content than %s", f.decl.Path)
	}
	return nil
}

func blobID(sha string, size int64) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(sha+":"+strconv.FormatInt(size, 10))).String()
}

func (c *fakeCloud) CompleteUpload(ctx context.Context, ref UploadRef) (UploadResult, error) {
	if err := ctx.Err(); err != nil {
		return UploadResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("complete")
	if up, p := c.upload(ref); p == nil && up.state == fakeCompleted {
		return up.result, nil
	}
	up, p := c.openUpload(ref)
	if p != nil {
		return UploadResult{}, p
	}
	for _, id := range up.order {
		if s := up.files[id].state; s != stateVerified && s != stateReused {
			return UploadResult{}, &ProblemError{
				Status: http.StatusConflict,
				Slug:   "volume-upload-incomplete",
				Detail: "a declared file is not verified",
			}
		}
	}
	changed := false
	for _, id := range up.order {
		f := up.files[id]
		e := fakeEntry{
			Mode:   f.decl.Mode,
			Size:   f.decl.Size,
			SHA256: f.decl.SHA256,
			BlobID: blobID(f.decl.SHA256, f.decl.Size),
		}
		if cur, ok := up.vol.files[f.decl.Path]; !ok || cur != e {
			up.vol.files[f.decl.Path] = e
			changed = true
		}
	}
	up.state = fakeCompleted
	up.result = UploadResult{
		Upload: Upload{
			ID:        up.id,
			VolumeID:  up.vol.id,
			State:     up.state,
			ExpiresAt: up.expiresAt,
		},
		Changed:        changed,
		ManifestSHA256: c.digest(up.vol),
	}
	return up.result, nil
}

func (c *fakeCloud) AbortUpload(ctx context.Context, ref UploadRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logCall("abort")
	up, p := c.upload(ref)
	if p != nil {
		return p
	}
	switch up.state {
	case fakeAborted:
		return nil
	case fakeOpen:
		up.state = fakeAborted
		return nil
	default:
		return &ProblemError{
			Status: http.StatusConflict,
			Slug:   slugUploadNotOpen,
			Detail: "the session was completed or has expired",
		}
	}
}

// uploadState is the state of the most recent session, or "" when none opened.
func (c *fakeCloud) uploadState() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		return ""
	}
	return c.last.state
}

func (c *fakeCloud) pathOf(fileID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f, ok := c.files[fileID]; ok {
		return f.decl.Path
	}
	return ""
}

func (c *fakeCloud) writeProblem(w http.ResponseWriter, status int, slug string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"type":   "https://docs.runware.ai/serverless/errors#" + slug,
		"title":  http.StatusText(status),
		"status": status,
		"detail": slug,
	}); err != nil {
		c.t.Error(err)
	}
}

// servePart is the fake upload endpoint.
func (c *fakeCloud) servePart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		c.writeProblem(w, http.StatusMethodNotAllowed, "method-not-allowed")
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	fields := strings.Split(token, ".")
	if len(fields) != 5 || fields[0] != "tok" {
		c.writeProblem(w, http.StatusUnauthorized, slugPartTokenInvalid)
		return
	}
	part, _ := strconv.Atoi(fields[2])
	length, _ := strconv.ParseInt(fields[3], 10, 64)
	expMilli, _ := strconv.ParseInt(fields[4], 10, 64)

	c.mu.Lock()
	f, ok := c.files[fields[1]]
	var attempt int
	if ok {
		for _, p := range c.puts {
			if p.FileID == f.id && p.Part == part {
				attempt++
			}
		}
		attempt++
	}
	c.mu.Unlock()
	if !ok {
		c.writeProblem(w, http.StatusUnauthorized, slugPartTokenInvalid)
		return
	}
	rq := partRequest{
		FileID:  f.id,
		Path:    f.decl.Path,
		Part:    part,
		Attempt: attempt,
	}
	if c.onPut != nil {
		c.onPut(rq)
	}
	if time.Now().UnixMilli() >= expMilli {
		c.record(rq, http.StatusUnauthorized, nil)
		c.writeProblem(w, http.StatusUnauthorized, slugPartTokenExpired)
		return
	}
	if r.ContentLength != length {
		c.record(rq, http.StatusBadRequest, nil)
		c.writeProblem(w, http.StatusBadRequest, slugPartLengthMismatch)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil || int64(len(body)) != length {
		c.record(rq, http.StatusBadRequest, nil)
		c.writeProblem(w, http.StatusBadRequest, slugPartLengthMismatch)
		return
	}
	salt := slices.Clone(body[:min(len(body), volumeformat.SaltBytes)])
	if c.fault != nil {
		if fault := c.fault(rq); fault != nil {
			c.record(rq, fault.Status, salt)
			c.writeProblem(w, fault.Status, fault.Slug)
			return
		}
	}
	sum := sha256.Sum256(body)
	ack := fmt.Sprintf("%s.%s.%d.%x", fakeAckPrefix, f.id, part, sum[:8])
	c.mu.Lock()
	f.parts[part] = storedPart{
		ciphertext: body,
		ack:        ack,
	}
	c.acks = append(c.acks, ack)
	c.mu.Unlock()
	c.record(rq, http.StatusOK, salt)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"partNumber": part,
		"ack":        ack,
	}); err != nil {
		c.t.Error(err)
	}
}

func (c *fakeCloud) record(rq partRequest, status int, salt []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.puts = append(c.puts, putRecord{
		FileID: rq.FileID,
		Path:   rq.Path,
		Part:   rq.Part,
		Status: status,
		Salt:   salt,
	})
}
