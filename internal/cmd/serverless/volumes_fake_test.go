package serverless

import (
	"bytes"
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/coredump"
	"github.com/runware/runware-cli/internal/volumes/put"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

// Marker values the fake answers with for every secret the upload handles, so
// a test can look for them in what the CLI printed.
const (
	markerToken = "PARTTOKENMARKER"
	markerAck   = "PARTACKMARKER"
	markerURL   = "UPLOADURLMARKER"
)

const (
	fakeSigningVersion = 1
	fakeStatementLabel = "runware-volumes-v1/key-pair-statement"
	fakeProblemBase    = "https://docs.runware.ai/serverless/errors#"
	fakeOpen           = "open"
	fakeCompleted      = "completed"
	fakeAborted        = "aborted"
	fakeUploading      = "uploading"
	fakeReused         = "reused"
	fakeVerified       = "verified"
	slugNotFound       = "not-found"
	slugLimitExceeded  = "volume-limit-exceeded"
	slugPathConflict   = "volume-upload-path-conflict"
	slugPartRefused    = "volume-part-token-invalid"
	slugCRCMismatch    = "volume-upload-crc-mismatch"
	memberDetail       = "detail"
	memberErrors       = "errors"
	memberLimit        = "limit"
	memberStatus       = "status"
	segVolumes         = "volumes"
	segFiles           = "files"
	putUse             = "put"
)

// fakeLimit is a limit the fake refuses a declaration with.
type fakeLimit struct {
	name string
	cap  int64
}

type fakeVolume struct {
	files    map[string]put.VolumeFile
	manifest string
}

type fakeUploadFile struct {
	id     string
	decl   put.FileDeclaration
	parts  int64
	state  string
	reused bool
}

type fakeUpload struct {
	id     string
	volume string
	state  string
	files  map[string]*fakeUploadFile
}

// putFake is the Volumes API and the upload endpoint in one, keeping state
// across runs so a test can upload twice.
type putFake struct {
	t        *testing.T
	mu       sync.Mutex
	api      *httptest.Server
	upload   *httptest.Server
	org      uuid.UUID
	signer   *ecdsa.PrivateKey
	pubKey   []byte
	keyPair  uuid.UUID
	sequence uint64
	home     string

	volumes  map[string]*fakeVolume
	blobs    map[string]struct{}
	uploads  map[string]*fakeUpload
	tokens   map[string]struct{}
	acks     map[string]struct{}
	calls    []string
	secrets  []string
	nextPart int
	keysDirs []string
	// keysBaseURLs are the base URLs the statement keys were chosen for.
	keysBaseURLs []string
	// transportBaseURLs are the base URLs the part transport was made for.
	transportBaseURLs []string

	// Knobs a test sets before a run.
	pageSize           int
	notAfter           time.Time
	statementOrg       uuid.UUID
	badSignature       bool
	maxFileBytes       int64
	limitOnDeclare     *fakeLimit
	closeOnDeclare     bool
	refusePart         bool
	partRefusalStatus  int // the status of a refused part; 403 when zero
	refuseComplete     bool
	closeOnComplete    bool // file completion answers that the session is not open
	missingOnDeclare   bool // declaration answers that the Volume is not found
	missingOnComplete  bool // file completion answers that the file is not found
	changeBetweenPages bool
	pagesChanged       bool
	onDeclare          func()
	coreDumpsOff       []bool
}

func newPutFake(t *testing.T) *putFake {
	t.Helper()
	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &putFake{
		t:        t,
		org:      uuid.New(),
		signer:   signer,
		pubKey:   vectorPublicKey(t),
		keyPair:  uuid.New(),
		sequence: 1,
		home:     t.TempDir(),
		volumes:  make(map[string]*fakeVolume),
		blobs:    make(map[string]struct{}),
		uploads:  make(map[string]*fakeUpload),
		tokens:   make(map[string]struct{}),
		acks:     make(map[string]struct{}),
		notAfter: time.Now().Add(time.Hour),
	}
	f.statementOrg = f.org
	f.api = httptest.NewServer(http.HandlerFunc(f.serveAPI))
	f.upload = httptest.NewTLSServer(http.HandlerFunc(f.servePart))
	t.Cleanup(f.api.Close)
	t.Cleanup(f.upload.Close)

	// Nothing reaches a real credential or service.
	t.Setenv("HOME", f.home)
	t.Setenv("USERPROFILE", f.home) // os.UserHomeDir reads it on Windows
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", f.api.URL)
	t.Setenv(config.OrganizationFromEnvVar, f.org.String())
	t.Setenv("RUNWARE_ENV", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if err := config.Init(); err != nil {
		t.Fatal(err)
	}

	oldKeys, oldTransport := newVolumeKeys, newPartTransport
	newVolumeKeys = func(baseURL, dir string) (put.Keys, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.keysBaseURLs = append(f.keysBaseURLs, baseURL)
		f.keysDirs = append(f.keysDirs, dir)
		return fakeKeys{signer: &signer.PublicKey}, nil
	}
	client := f.upload.Client()
	newPartTransport = func(baseURL string, logger *slog.Logger) put.Transport {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.transportBaseURLs = append(f.transportBaseURLs, baseURL)
		return newHTTPPartTransport(client, baseURL, logger)
	}
	t.Cleanup(func() { newVolumeKeys, newPartTransport = oldKeys, oldTransport })
	return f
}

type fakeVectorFile struct {
	KeyPair fakeVectorKeyPair `json:"keyPair"`
}

type fakeVectorKeyPair struct {
	PublicKey string `json:"publicKey"`
}

// vectorPublicKey is the format vectors' key-pair public key.
func vectorPublicKey(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "volumeformat", "testdata", "format-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw fakeVectorFile
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(raw.KeyPair.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (f *putFake) newVolume(env, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumes[env+"/"+name] = &fakeVolume{files: make(map[string]put.VolumeFile)}
}

// volume returns a copy of the Volume's latest files and their digest.
func (f *putFake) volume(env, name string) (map[string]put.VolumeFile, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.volumes[env+"/"+name]
	return maps.Clone(v.files), v.manifest
}

func (f *putFake) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// sentSecrets is every secret the CLI sent: sealed keys and encapsulations.
func (f *putFake) sentSecrets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.secrets)
}

// statement encodes and signs the key-pair statement the knobs describe.
func (f *putFake) statement() put.KeyPairStatement {
	field := func(b []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(b))) //nolint:gosec // test fields are small
		return append(out, b...)
	}
	u32 := binary.BigEndian.AppendUint32(nil, fakeSigningVersion)
	u64 := func(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
	var st []byte
	for _, b := range [][]byte{
		[]byte(fakeStatementLabel),
		[]byte(volumeformat.KEMIdentifier),
		u32,
		f.statementOrg[:],
		f.keyPair[:],
		u64(f.sequence),
		u64(uint64(time.Now().Add(-time.Minute).Unix())), //nolint:gosec // a positive time
		u64(uint64(f.notAfter.Unix())),                   //nolint:gosec // a positive time
		f.pubKey,
	} {
		st = append(st, field(b)...)
	}
	signer := f.signer
	if f.badSignature {
		other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			f.t.Fatal(err)
		}
		signer = other
	}
	digest := sha256.Sum256(st)
	sig, err := ecdsa.SignASN1(rand.Reader, signer, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return put.KeyPairStatement{
		Statement: base64.RawURLEncoding.EncodeToString(st),
		Signature: base64.RawURLEncoding.EncodeToString(sig),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, slug, detail string, extra map[string]any) {
	body := map[string]any{
		"type":       fakeProblemBase + slug,
		"title":      http.StatusText(status),
		memberStatus: status,
		memberDetail: detail,
	}
	maps.Copy(body, extra)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func pointerTo(pointer string) map[string]any {
	return map[string]any{
		memberErrors: []map[string]string{
			{
				memberDetail: "refused",
				"pointer":    pointer,
			},
		},
	}
}

func (f *putFake) serveAPI(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env := r.URL.Query().Get("environment")
	call := r.Method + " " + r.URL.Path
	if env != "" {
		call += "?environment=" + env
	}
	f.calls = append(f.calls, call)
	if r.Header.Get("Authorization") != "Bearer test-key" {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "no key", nil)
		return
	}
	seg := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
	if len(seg) == 1 && seg[0] == "volumes-key-pair" && r.Method == http.MethodGet {
		off, err := coredump.Disabled()
		f.coreDumpsOff = append(f.coreDumpsOff, off && err == nil)
		writeJSON(w, http.StatusOK, f.statement())
		return
	}
	if len(seg) < 3 || seg[0] != segVolumes {
		writeProblem(w, http.StatusNotFound, slugNotFound, "no route", nil)
		return
	}
	volKey := env + "/" + seg[1]
	vol, ok := f.volumes[volKey]
	if !ok {
		writeProblem(w, http.StatusNotFound, slugNotFound, "volume not found", nil)
		return
	}
	switch {
	case len(seg) == 3 && seg[2] == segFiles && r.Method == http.MethodGet:
		f.listFiles(w, r, vol)
	case len(seg) == 3 && seg[2] == "uploads" && r.Method == http.MethodPost:
		f.openUpload(w, r, volKey)
	case len(seg) >= 4 && seg[2] == "uploads":
		up, ok := f.uploads[seg[3]]
		if !ok || up.volume != volKey {
			writeProblem(w, http.StatusNotFound, slugNotFound, "upload not found", nil)
			return
		}
		f.serveUpload(w, r, vol, up, seg[4:])
	default:
		writeProblem(w, http.StatusNotFound, slugNotFound, "no route", nil)
	}
}

func (f *putFake) serveUpload(w http.ResponseWriter, r *http.Request, vol *fakeVolume, up *fakeUpload, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodDelete:
		if up.state == fakeCompleted {
			writeProblem(w, http.StatusConflict, "volume-upload-not-open", "the upload session is not open", nil)
			return
		}
		up.state = fakeAborted
		w.WriteHeader(http.StatusNoContent)
	case len(rest) == 1 && rest[0] == segFiles && r.Method == http.MethodPost:
		f.declare(w, r, vol, up)
	case len(rest) == 3 && rest[0] == segFiles && rest[2] == "parts" && r.Method == http.MethodPost:
		file, ok := up.files[rest[1]]
		if !ok {
			writeProblem(w, http.StatusNotFound, slugNotFound, "file not found", nil)
			return
		}
		var req put.PartTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad-request", "bad body", nil)
			return
		}
		targets := make([]put.Target, 0, len(req.PartNumbers))
		for _, n := range req.PartNumbers {
			targets = append(targets, f.target(file.id, n))
		}
		writeJSON(w, http.StatusOK, put.UploadTargets{UploadTargets: targets})
	case len(rest) == 3 && rest[0] == segFiles && rest[2] == "complete" && r.Method == http.MethodPost:
		f.completeFile(w, r, up, rest[1])
	case len(rest) == 1 && rest[0] == "complete" && r.Method == http.MethodPost:
		f.completeUpload(w, vol, up)
	default:
		writeProblem(w, http.StatusNotFound, slugNotFound, "no route", nil)
	}
}

func (f *putFake) listFiles(w http.ResponseWriter, r *http.Request, vol *fakeVolume) {
	paths := slices.Sorted(maps.Keys(vol.files))
	size := f.pageSize
	if size <= 0 {
		size = len(paths) + 1
	}
	start := 0
	if c := r.URL.Query().Get("cursor"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "bad-request", "bad cursor", nil)
			return
		}
		start = n
	}
	end := min(start+size, len(paths))
	page := map[string]any{
		"data": []put.VolumeFile{},
	}
	data := make([]put.VolumeFile, 0, end-start)
	for _, p := range paths[start:end] {
		data = append(data, vol.files[p])
	}
	page["data"] = data
	manifest := vol.manifest
	if start > 0 && f.changeBetweenPages && !f.pagesChanged {
		f.pagesChanged = true
		manifest = strings.Repeat("0", 64)
	}
	if manifest != "" {
		page["manifestSha256"] = manifest
	}
	if end < len(paths) {
		page["nextCursor"] = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, page)
}

func (f *putFake) openUpload(w http.ResponseWriter, r *http.Request, volKey string) {
	var open put.UploadOpen
	if err := json.NewDecoder(r.Body).Decode(&open); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad-request", "bad body", nil)
		return
	}
	f.secrets = append(f.secrets, open.Encapsulation)
	up := &fakeUpload{
		id:     uuid.NewString(),
		volume: volKey,
		state:  fakeOpen,
		files:  make(map[string]*fakeUploadFile),
	}
	f.uploads[up.id] = up
	now := time.Now().UTC()
	writeJSON(w, http.StatusCreated, put.Upload{
		ID:        up.id,
		VolumeID:  uuid.NewString(),
		State:     fakeOpen,
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	})
}

func blobKey(sha string, size int64) string { return sha + ":" + strconv.FormatInt(size, 10) }

func pathConflict(files map[string]put.VolumeFile, path string) bool {
	for p := range files {
		if strings.HasPrefix(path, p+"/") || strings.HasPrefix(p, path+"/") {
			return true
		}
	}
	return false
}

func (f *putFake) declare(w http.ResponseWriter, r *http.Request, vol *fakeVolume, up *fakeUpload) {
	var decls put.FileDeclarations
	if err := json.NewDecoder(r.Body).Decode(&decls); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad-request", "bad body", nil)
		return
	}
	if f.missingOnDeclare {
		writeProblem(w, http.StatusNotFound, slugNotFound, "volume not found", nil)
		return
	}
	if f.closeOnDeclare {
		up.state = fakeCompleted
	}
	if up.state != fakeOpen {
		writeProblem(w, http.StatusConflict, "volume-upload-not-open", "the upload session is not open", nil)
		return
	}
	if f.limitOnDeclare != nil {
		writeProblem(w, http.StatusConflict, slugLimitExceeded, "a limit would be exceeded", map[string]any{
			memberLimit: f.limitOnDeclare.name,
			"cap":       f.limitOnDeclare.cap,
		})
		return
	}
	for i, d := range decls.Files {
		f.secrets = append(f.secrets, d.SealedKey)
		if f.maxFileBytes > 0 && d.Size > f.maxFileBytes {
			extra := pointerTo(fmt.Sprintf("/files/%d/size", i))
			extra[memberLimit] = "maxFileBytes"
			extra["cap"] = f.maxFileBytes
			writeProblem(w, http.StatusConflict, slugLimitExceeded, "maxFileBytes exceeds its limit", extra)
			return
		}
		if pathConflict(vol.files, d.Path) {
			writeProblem(w, http.StatusConflict, slugPathConflict,
				"the path and a path in the latest files or this session are a file and a directory; no upload removes a file",
				pointerTo(fmt.Sprintf("/files/%d/path", i)))
			return
		}
	}
	answers := make([]put.DeclarationAnswer, 0, len(decls.Files))
	for _, d := range decls.Files {
		file := &fakeUploadFile{
			id:   uuid.NewString(),
			decl: d,
		}
		up.files[file.id] = file
		answer := put.DeclarationAnswer{
			File: put.UploadFile{
				ID:     file.id,
				Path:   d.Path,
				Mode:   d.Mode,
				Size:   d.Size,
				SHA256: d.SHA256,
			},
		}
		if _, ok := f.blobs[blobKey(d.SHA256, d.Size)]; ok {
			file.reused, file.state = true, fakeReused
			answer.File.State = fakeReused
			answer.Blob = &put.BlobReference{
				SHA256: d.SHA256,
				Size:   d.Size,
			}
		} else {
			layout, err := volumeformat.PlanLayout(d.Size)
			if err != nil {
				f.t.Errorf("plan layout: %v", err)
				writeProblem(w, http.StatusInternalServerError, "internal", "layout", nil)
				return
			}
			file.parts, file.state = layout.PartCount, fakeUploading
			answer.File.State = fakeUploading
			answer.File.Layout = &put.PartLayout{
				SegmentBytes:          volumeformat.SegmentBytes,
				SegmentPlaintextBytes: volumeformat.SegmentPlaintextBytes,
				SegmentsPerPart:       layout.SegmentsPerPart,
				PartSizeBytes:         layout.PartSize,
				PartCount:             layout.PartCount,
				CiphertextBytes:       layout.CiphertextSize,
			}
			for n := 1; int64(n) <= layout.PartCount; n++ {
				answer.UploadTargets = append(answer.UploadTargets, f.target(file.id, n))
			}
		}
		answers = append(answers, answer)
	}
	if f.onDeclare != nil {
		f.onDeclare()
	}
	writeJSON(w, http.StatusOK, put.DeclarationResult{Files: answers})
}

// target issues a part target whose address and token are markers.
func (f *putFake) target(fileID string, part int) put.Target {
	f.nextPart++
	token := fmt.Sprintf("%s-%d", markerToken, f.nextPart)
	f.tokens[token] = struct{}{}
	return put.Target{
		PartNumber: part,
		URL:        fmt.Sprintf("%s/parts/%s-%s/%d", f.upload.URL, markerURL, fileID, part),
		Token:      put.Secret(token),
		ExpiresAt:  time.Now().Add(time.Hour),
	}
}

func (f *putFake) servePart(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if _, ok := f.tokens[token]; !ok || r.Method != http.MethodPut || f.refusePart {
		writeProblem(w, cmp.Or(f.partRefusalStatus, http.StatusForbidden), slugPartRefused, "the part token was refused", nil)
		return
	}
	part, err := strconv.Atoi(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
	if err != nil || int64(len(body)) != r.ContentLength {
		writeProblem(w, http.StatusBadRequest, "bad-request", "bad part", nil)
		return
	}
	ack := fmt.Sprintf("%s-%d", markerAck, len(f.acks)+1)
	f.acks[ack] = struct{}{}
	writeJSON(w, http.StatusOK, map[string]any{
		"partNumber": part,
		"ack":        ack,
	})
}

func (f *putFake) completeFile(w http.ResponseWriter, r *http.Request, up *fakeUpload, fileID string) {
	file, ok := up.files[fileID]
	if !ok {
		writeProblem(w, http.StatusNotFound, slugNotFound, "file not found", nil)
		return
	}
	var c put.FileCompletion
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad-request", "bad body", nil)
		return
	}
	if f.missingOnComplete {
		writeProblem(w, http.StatusNotFound, slugNotFound, "file not found", nil)
		return
	}
	if f.closeOnComplete {
		writeProblem(w, http.StatusConflict, "volume-upload-not-open", "the upload session is not open", nil)
		return
	}
	if f.refuseComplete {
		writeProblem(w, http.StatusConflict, slugCRCMismatch, "the ciphertext checksum does not match", nil)
		return
	}
	if int64(len(c.Parts)) != file.parts {
		writeProblem(w, http.StatusConflict, "volume-upload-parts-invalid", "missing parts", nil)
		return
	}
	for _, p := range c.Parts {
		if _, ok := f.acks[string(p.Ack)]; !ok {
			writeProblem(w, http.StatusConflict, "volume-upload-parts-invalid", "unknown acknowledgement", nil)
			return
		}
	}
	file.state = fakeVerified
	f.blobs[blobKey(file.decl.SHA256, file.decl.Size)] = struct{}{}
	writeJSON(w, http.StatusOK, put.UploadFile{
		ID:     file.id,
		Path:   file.decl.Path,
		Mode:   file.decl.Mode,
		Size:   file.decl.Size,
		SHA256: file.decl.SHA256,
		State:  fakeVerified,
	})
}

func (f *putFake) completeUpload(w http.ResponseWriter, vol *fakeVolume, up *fakeUpload) {
	if up.state != fakeOpen {
		writeProblem(w, http.StatusConflict, "volume-upload-not-open", "the upload session is not open", nil)
		return
	}
	for _, file := range up.files {
		if file.state != fakeVerified && file.state != fakeReused {
			writeProblem(w, http.StatusConflict, "volume-upload-incomplete", "a file is not verified", nil)
			return
		}
	}
	for _, file := range up.files {
		vol.files[file.decl.Path] = put.VolumeFile{
			Path:   file.decl.Path,
			Mode:   file.decl.Mode,
			Size:   file.decl.Size,
			SHA256: file.decl.SHA256,
		}
	}
	before := vol.manifest
	vol.manifest = manifestOf(vol.files)
	up.state = fakeCompleted
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, put.UploadResult{
		Upload: put.Upload{
			ID:        up.id,
			State:     fakeCompleted,
			CreatedAt: now,
			ExpiresAt: now.Add(24 * time.Hour),
		},
		Changed:        vol.manifest != before,
		ManifestSHA256: vol.manifest,
	})
}

func manifestOf(files map[string]put.VolumeFile) string {
	if len(files) == 0 {
		return ""
	}
	h := sha256.New()
	for _, p := range slices.Sorted(maps.Keys(files)) {
		v := files[p]
		_, _ = fmt.Fprintf(h, "%s\t%d\t%d\t%s\n", v.Path, v.Mode, v.Size, v.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// putRun is what one volumes put printed and returned.
type putRun struct {
	stdout string
	stderr string
	err    error
}

// output is everything the run showed the user, the error included.
func (r putRun) output() string {
	s := r.stdout + r.stderr
	if r.err != nil {
		s += r.err.Error()
	}
	return s
}

// run runs `serverless volumes put <args>` with --format format, logging at
// debug level to the same standard error when verbose is set.
func (f *putFake) run(format string, verbose bool, args ...string) putRun {
	f.t.Helper()
	var stdout, stderr bytes.Buffer
	logger := log.New(&stderr)
	if verbose {
		logger.SetLevel(log.DebugLevel)
	}
	cmd := NewCmd(logger)
	cmd.PersistentFlags().String("format", format, "")
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{segVolumes, putUse}, args...))
	err := cmd.ExecuteContext(context.Background())
	return putRun{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

// newTree writes files (path to content) under a new directory and returns it.
func newTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	return dir
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
