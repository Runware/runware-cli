package keypair

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

const (
	testVersion     = 1
	testNextVersion = 2
	testHost        = "api.example.com"
	testOtherHost   = "api.example.net"
)

var (
	testOrg      = [16]byte(uuid.MustParse("0b9d2c6e-5a1f-4c3e-9e7a-2f4b8d1c6a35"))
	testKeyPair  = [16]byte(uuid.MustParse("c4a1f8e2-3b6d-4e9a-8c57-1d2f0e9b6a73"))
	testIssuedAt = time.Date(2026, time.November, 10, 0, 0, 0, 0, time.UTC)
	testNotAfter = testIssuedAt.Add(14 * 24 * time.Hour)
	testNow      = testIssuedAt.Add(24 * time.Hour)
)

type vectorFile struct {
	KeyPair   vectorKeyPair   `json:"keyPair"`
	Session   vectorSession   `json:"session"`
	Statement vectorStatement `json:"statement"`
	Files     []vectorCase    `json:"files"`
}

type vectorKeyPair struct {
	Organization string `json:"organization"`
	KeyPairID    string `json:"keyPairId"`
	PrivateKey   string `json:"privateKey"`
	PublicKey    string `json:"publicKey"`
}

type vectorSession struct {
	UploadID string `json:"uploadId"`
}

type vectorStatement struct {
	Encoded              string `json:"encoded"`
	IssuedAt             int64  `json:"issuedAt"`
	NotAfter             int64  `json:"notAfter"`
	Sequence             uint64 `json:"sequence"`
	Signature            string `json:"signature"`
	SigningKeyVersion    uint32 `json:"signingKeyVersion"`
	SigningPublicKeySpki string `json:"signingPublicKeySpki"`
}

type vectorCase struct {
	Name      string          `json:"name"`
	SHA256    string          `json:"sha256"`
	Plaintext vectorPlaintext `json:"plaintext"`
}

type vectorPlaintext struct {
	Size uint64 `json:"size"`
}

// vectors is the shared format-vectors file, decoded.
type vectors struct {
	raw        vectorFile
	org        [16]byte
	keyPair    [16]byte
	upload     [16]byte
	privateKey []byte
	publicKey  []byte
	statement  []byte
	signature  []byte
	signingKey *ecdsa.PublicKey
	issuedAt   time.Time
	notAfter   time.Time
}

func loadVectors(t *testing.T) *vectors {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "volumeformat", "testdata", "format-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw vectorFile
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParsePKIXPublicKey(unhex(t, raw.Statement.SigningPublicKeySpki))
	if err != nil {
		t.Fatal(err)
	}
	signingKey, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("the vectors' signing key is not an ECDSA key")
	}
	return &vectors{
		raw:        raw,
		org:        [16]byte(uuid.MustParse(raw.KeyPair.Organization)),
		keyPair:    [16]byte(uuid.MustParse(raw.KeyPair.KeyPairID)),
		upload:     [16]byte(uuid.MustParse(raw.Session.UploadID)),
		privateKey: unhex(t, raw.KeyPair.PrivateKey),
		publicKey:  unhex(t, raw.KeyPair.PublicKey),
		statement:  unhex(t, raw.Statement.Encoded),
		signature:  unhex(t, raw.Statement.Signature),
		signingKey: signingKey,
		issuedAt:   time.Unix(raw.Statement.IssuedAt, 0).UTC(),
		notAfter:   time.Unix(raw.Statement.NotAfter, 0).UTC(),
	}
}

// verifier returns a Verifier holding the vectors' signing key, clocked at now, and its config directory.
func (v *vectors) verifier(t *testing.T, now time.Time) (*Verifier, string) {
	t.Helper()
	return newTestVerifier(t, v.raw.Statement.SigningKeyVersion, v.signingKey, now)
}

// verified returns the vectors' statement as Verify accepts it.
func (v *vectors) verified(t *testing.T) Statement {
	t.Helper()
	verifier, _ := v.verifier(t, v.issuedAt.Add(time.Hour))
	st, err := verifier.Verify(v.statement, v.signature, v.org)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// session opens a session on the vectors' key pair.
func (v *vectors) session(t *testing.T) *Session {
	t.Helper()
	s, err := Open(v.verified(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newTestVerifier(t *testing.T, version uint32, key *ecdsa.PublicKey, now time.Time) (*Verifier, string) {
	t.Helper()
	return newVerifierWithKeys(t, map[uint32]*ecdsa.PublicKey{version: key}, now)
}

func newVerifierWithKeys(t *testing.T, keys map[uint32]*ecdsa.PublicKey, now time.Time) (*Verifier, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	verifier, err := newVerifier(keys, testHost, dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return verifier, dir
}

func newReleaseKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func sign(t *testing.T, key *ecdsa.PrivateKey, statement []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(statement)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

// statementSpec is a statement's contents; fields encodes them as the shared vectors' statement is.
type statementSpec struct {
	version   uint32
	org       [16]byte
	keyPair   [16]byte
	sequence  uint64
	issuedAt  time.Time
	notAfter  time.Time
	publicKey []byte
}

func (s statementSpec) fields() [][]byte {
	return [][]byte{
		[]byte(statementLabel),
		[]byte(volumeformat.KEMIdentifier),
		binary.BigEndian.AppendUint32(nil, s.version),
		slices.Clone(s.org[:]),
		slices.Clone(s.keyPair[:]),
		binary.BigEndian.AppendUint64(nil, s.sequence),
		binary.BigEndian.AppendUint64(nil, uint64(s.issuedAt.Unix())),
		binary.BigEndian.AppendUint64(nil, uint64(s.notAfter.Unix())),
		slices.Clone(s.publicKey),
	}
}

func join(fields [][]byte) []byte {
	var b []byte
	for _, f := range fields {
		b = binary.BigEndian.AppendUint32(b, uint32(len(f)))
		b = append(b, f...)
	}
	return b
}

func (v *vectors) spec() statementSpec {
	return statementSpec{
		version:   v.raw.Statement.SigningKeyVersion,
		org:       v.org,
		keyPair:   v.keyPair,
		sequence:  v.raw.Statement.Sequence,
		issuedAt:  v.issuedAt,
		notAfter:  v.notAfter,
		publicKey: v.publicKey,
	}
}

// readMarks returns the highest sequence per mark in the record in dir, or nil when there is none.
func readMarks(t *testing.T, dir string) map[string]uint64 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, sequenceFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	marks := map[string]uint64{}
	for line := range bytes.Lines(data) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m sequenceMark
		if err := json.Unmarshal(line, &m); err != nil || !bytes.HasSuffix(line, []byte("\n")) {
			t.Fatalf("record line %q is not a whole JSON line: %v", line, err)
		}
		key := markKey(m.Host, m.Organization)
		marks[key] = max(marks[key], m.Sequence)
	}
	return marks
}

func markKey(host string, org [16]byte) string { return host + "/" + uuid.UUID(org).String() }

func orgKey(org [16]byte) string { return markKey(testHost, org) }

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func allZero(b []byte) bool {
	return !slices.ContainsFunc(b, func(c byte) bool { return c != 0 })
}
