package keypair

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

// boundFields are the session id and file identity a sealed key's associated data binds.
type boundFields struct {
	session [16]byte
	id      volumeformat.FileIdentity
}

// fileOf is the digest and size of a vector file.
func fileOf(t *testing.T, c vectorCase) ([32]byte, uint64) {
	t.Helper()
	return [32]byte(unhex(t, c.SHA256)), c.Plaintext.Size
}

func sealedKeyFixture(t *testing.T) (*vectors, *Session, []byte) {
	t.Helper()
	v := loadVectors(t)
	s := v.session(t)
	if err := s.Bind(v.upload); err != nil {
		t.Fatal(err)
	}
	return v, s, bytes.Clone(s.Key())
}

// TestOpenSessionRefusesBeforeEncapsulating: no session, and so nothing sealed, comes from a
// statement Verify did not accept, including the zero value a refused Verify returns.
func TestOpenSessionRefusesBeforeEncapsulating(t *testing.T) {
	v := loadVectors(t)
	verifier, _ := v.verifier(t, v.notAfter)

	refused, err := verifier.Verify(v.statement, v.signature, v.org)
	if !errors.Is(err, ErrStatement) {
		t.Fatalf("Verify at the not-after: error = %v, want ErrStatement", err)
	}
	handBuilt := Statement{
		SigningKeyVersion: v.raw.Statement.SigningKeyVersion,
		Organization:      v.org,
		KeyPair:           v.keyPair,
		Sequence:          v.raw.Statement.Sequence,
		IssuedAt:          v.issuedAt,
		NotAfter:          v.notAfter,
	}
	for name, st := range map[string]Statement{
		"the zero Statement a refused Verify returns": refused,
		"a statement built by hand":                   handBuilt,
	} {
		s, err := Open(st)
		if !errors.Is(err, ErrStatement) {
			t.Errorf("%s: Open error = %v, want ErrStatement", name, err)
		}
		if s != nil {
			t.Errorf("%s: Open returned a session", name)
		}
	}
}

// TestOpenFailsWhenTheKeyDoesNotEncapsulate: a validly signed statement can still carry bytes
// that are no key pair's public key; Open returns the encapsulation error and no session.
func TestOpenFailsWhenTheKeyDoesNotEncapsulate(t *testing.T) {
	v := loadVectors(t)
	key := newReleaseKey(t)
	verifier, _ := newTestVerifier(t, testVersion, &key.PublicKey, testNow)
	spec := baseSpec(v)
	spec.publicKey = bytes.Repeat([]byte{0xff}, volumeformat.PublicKeyBytes)
	statement := join(spec.fields())
	st, err := verifier.Verify(statement, sign(t, key, statement), testOrg)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Open(st)

	if err == nil || !strings.Contains(err.Error(), "encapsulate to the key pair") {
		t.Fatalf("Open error = %v, want the encapsulation error", err)
	}
	if s != nil {
		t.Fatal("Open returned a session for a key it could not encapsulate to")
	}
}

// TestEncapsulationIsACopy: writing to the encapsulation a caller was handed does not change
// the session's.
func TestEncapsulationIsACopy(t *testing.T) {
	v := loadVectors(t)
	s := v.session(t)
	want := bytes.Clone(s.Encapsulation())

	clear(s.Encapsulation())

	if !bytes.Equal(s.Encapsulation(), want) {
		t.Fatal("a write to the returned encapsulation changed the session's")
	}
}

// TestSealedKeyUnwrapsWithVectors: against the shared vectors' key pair, organization and
// upload id, a key the CLI seals for each vector file unwraps on the decrypting side with the
// expected associated data, and changing any bound field makes it fail.
func TestSealedKeyUnwrapsWithVectors(t *testing.T) {
	v, s, sessionKey := sealedKeyFixture(t)

	decrypting, err := volumeformat.OpenSessionKey(v.privateKey, s.Encapsulation(), v.org, v.keyPair, v.upload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypting, sessionKey) {
		t.Fatal("the CLI and the decrypting side derive different sealing keys")
	}
	if len(s.Encapsulation()) != volumeformat.EncapsulationBytes {
		t.Fatalf("encapsulation is %d bytes", len(s.Encapsulation()))
	}

	if len(v.raw.Files) == 0 {
		t.Fatal("the vectors list no files")
	}
	for _, c := range v.raw.Files {
		t.Run(c.Name, func(t *testing.T) {
			digest, size := fileOf(t, c)
			fk, err := s.SealFile(digest, size)
			if err != nil {
				t.Fatal(err)
			}
			id := fk.Identity()
			if id.Organization != v.org || id.SHA256 != digest || id.Size != size || id.Attempt != fk.Attempt() {
				t.Fatalf("identity %+v does not carry the file's organization, digest, size and attempt", id)
			}
			sealed := fk.Sealed()
			if len(sealed) != volumeformat.SealedKeyBytes {
				t.Fatalf("sealed key is %d bytes, want %d", len(sealed), volumeformat.SealedKeyBytes)
			}
			got, err := volumeformat.UnwrapDataKey(decrypting, v.upload, id, sealed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, fk.DataKey()) {
				t.Fatal("the sealed key opens to another data key")
			}

			for field, edit := range map[string]func(*boundFields){
				"organization": func(b *boundFields) { b.id.Organization[0] ^= 1 },
				"session":      func(b *boundFields) { b.session[0] ^= 1 },
				"sha256":       func(b *boundFields) { b.id.SHA256[0] ^= 1 },
				"size":         func(b *boundFields) { b.id.Size++ },
				"attempt":      func(b *boundFields) { b.id.Attempt[0] ^= 1 },
			} {
				changed := boundFields{
					session: v.upload,
					id:      id,
				}
				edit(&changed)
				key, err := volumeformat.UnwrapDataKey(decrypting, changed.session, changed.id, sealed)
				if !errors.Is(err, volumeformat.ErrAuthentication) || key != nil {
					t.Errorf("%s changed: key = %x, error = %v, want ErrAuthentication and no key", field, key, err)
				}
			}
		})
	}
}

// TestAttemptsAreFresh: every attempt of one file draws its own data key and attempt id, so
// even the sealed bytes differ.
func TestAttemptsAreFresh(t *testing.T) {
	v, s, sessionKey := sealedKeyFixture(t)
	digest, size := fileOf(t, v.raw.Files[1])

	first, err := s.SealFile(digest, size)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SealFile(digest, size)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.DataKey(), second.DataKey()) {
		t.Error("two attempts share a data key")
	}
	if first.Attempt() == second.Attempt() {
		t.Error("two attempts share an attempt id")
	}
	if bytes.Equal(first.Sealed(), second.Sealed()) {
		t.Error("two attempts share sealed bytes")
	}
	for i, fk := range []*FileKey{first, second} {
		if _, err := volumeformat.UnwrapDataKey(sessionKey, v.upload, fk.Identity(), fk.Sealed()); err != nil {
			t.Errorf("attempt %d does not unwrap: %v", i+1, err)
		}
	}

	dataKeys, attempts := map[string]struct{}{}, map[[16]byte]struct{}{}
	for range 200 {
		fk, err := s.SealFile(digest, size)
		if err != nil {
			t.Fatal(err)
		}
		dataKeys[string(fk.DataKey())] = struct{}{}
		attempts[fk.Attempt()] = struct{}{}
	}
	if len(dataKeys) != 200 || len(attempts) != 200 {
		t.Fatalf("200 attempts drew %d data keys and %d attempt ids", len(dataKeys), len(attempts))
	}
}

func TestSessionStates(t *testing.T) {
	v := loadVectors(t)
	digest, size := fileOf(t, v.raw.Files[0])
	s := v.session(t)

	if _, err := s.SealFile(digest, size); !errors.Is(err, ErrSessionState) {
		t.Fatalf("SealFile before Bind: error = %v, want ErrSessionState", err)
	}
	if s.Key() != nil {
		t.Fatal("Key before Bind is not nil")
	}
	if err := s.Bind(v.upload); err != nil {
		t.Fatal(err)
	}
	if err := s.Bind(v.upload); !errors.Is(err, ErrSessionState) {
		t.Fatalf("second Bind: error = %v, want ErrSessionState", err)
	}
	if _, err := s.SealFile(digest, size); err != nil {
		t.Fatal(err)
	}

	s.Clear()
	if _, err := s.SealFile(digest, size); !errors.Is(err, ErrSessionState) {
		t.Fatalf("SealFile after Clear: error = %v, want ErrSessionState", err)
	}
	if err := s.Bind(v.upload); !errors.Is(err, ErrSessionState) {
		t.Fatalf("Bind after Clear: error = %v, want ErrSessionState", err)
	}
	if s.Key() != nil {
		t.Fatal("Key after Clear is not nil")
	}
	s.Clear()

	abandoned := v.session(t)
	abandoned.Clear()
	if err := abandoned.Bind(v.upload); !errors.Is(err, ErrSessionState) {
		t.Fatalf("Bind after Clear on a session never bound: error = %v, want ErrSessionState", err)
	}
	if _, err := abandoned.SealFile(digest, size); !errors.Is(err, ErrSessionState) {
		t.Fatalf("SealFile after Clear on a session never bound: error = %v, want ErrSessionState", err)
	}
}

type journalEntry struct {
	File    *FileKey
	Session *Session
}

type heldByValue struct {
	file  FileKey
	files []FileKey
	sess  Session
}

type heldByPointer struct {
	file  *FileKey
	files []*FileKey
	sess  *Session
}

var fmtVerbs = []string{
	"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%o", "%b", "%c", "%U", "%e", "%f", "%t", "%08d", "%+d",
}

// TestEveryFmtVerbPrintsRedacted: whatever verb formats a session or a file key, passed by value
// or by pointer, the output is [redacted], and so is a direct String or GoString call.
func TestEveryFmtVerbPrintsRedacted(t *testing.T) {
	v, s, _ := sealedKeyFixture(t)
	digest, size := fileOf(t, v.raw.Files[1])
	fk, err := s.SealFile(digest, size)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{
		"*Session": s,
		"Session":  reflect.ValueOf(s).Elem().Interface(), // a copy; go vet bars *s for its mutex
		"*FileKey": fk,
		"FileKey":  *fk,
	}

	for name, arg := range args {
		for _, verb := range fmtVerbs {
			if fmt.Sprintf(verb, arg) != redactedText {
				t.Errorf("Sprintf(%q, %s) does not print %s", verb, name, redactedText)
			}
		}
		if arg.(fmt.Stringer).String() != redactedText || arg.(fmt.GoStringer).GoString() != redactedText {
			t.Errorf("%s: String or GoString does not return %s", name, redactedText)
		}
	}
}

// TestKeysInUnexportedFieldsPrintNoBytes: fmt calls no method on a value in an unexported field, so
// a Session or FileKey held there, alone, in a slice or by pointer, must print no key under any verb.
// What a struct prints must not change when the live keys are zeroed.
func TestKeysInUnexportedFieldsPrintNoBytes(t *testing.T) {
	v, s, _ := sealedKeyFixture(t)
	digest, size := fileOf(t, v.raw.Files[1])
	fk, err := s.SealFile(digest, size)
	if err != nil {
		t.Fatal(err)
	}
	sessionKey, dataKey := s.Key(), fk.DataKey()
	if allZero(sessionKey) || allZero(dataKey) {
		t.Fatal("a live key is all zero")
	}
	held := map[string]any{
		"by value": heldByValue{
			file:  *fk,
			files: []FileKey{*fk},
			sess: Session{
				org:     s.org,
				keyPair: s.keyPair,
				enc:     s.enc,
				session: s.session,
				key:     s.key,
			},
		},
		"by pointer": heldByPointer{
			file:  fk,
			files: []*FileKey{fk},
			sess:  s,
		},
	}
	printAll := func() map[string]string {
		printed := map[string]string{}
		for name, h := range held {
			for _, verb := range fmtVerbs {
				printed[verb+" "+name] = fmt.Sprintf(verb, h)
			}
		}
		return printed
	}

	before := printAll()
	clear(sessionKey)
	clear(dataKey)
	after := printAll()

	for name, text := range before {
		if after[name] != text {
			t.Errorf("%s prints the key bytes", name)
		}
	}
}

// TestKeysAreClearedAndRedacted: Clear zeroes the live keys the accessors handed out, and
// logging a session or file key, or printing one in an exported field, shows no key material.
func TestKeysAreClearedAndRedacted(t *testing.T) {
	v, s, _ := sealedKeyFixture(t)
	digest, size := fileOf(t, v.raw.Files[1])
	fk, err := s.SealFile(digest, size)
	if err != nil {
		t.Fatal(err)
	}
	sessionKey, dataKey := s.Key(), fk.DataKey()
	if allZero(sessionKey) || allZero(dataKey) {
		t.Fatal("a live key is all zero")
	}

	var secrets []string
	for _, key := range [][]byte{sessionKey, dataKey} {
		secrets = append(secrets, hex.EncodeToString(key), fmt.Sprintf("%d", key), base64.StdEncoding.EncodeToString(key))
	}
	var logs strings.Builder
	jsonLog := slog.New(slog.NewJSONHandler(&logs, nil))
	textLog := slog.New(slog.NewTextHandler(&logs, nil))
	jsonLog.Info("sealed", "session", s, "file", fk)
	textLog.Info("sealed", "session", s, "file", fk)
	if !strings.Contains(logs.String(), `"session":"[redacted]"`) || !strings.Contains(logs.String(), "file=[redacted]") {
		t.Errorf("logs do not redact: %s", logs.String())
	}
	printed := logs.String()
	entry := journalEntry{
		File:    fk,
		Session: s,
	}
	printed += fmt.Sprintf("%+v", entry) + s.String() + fk.String() + s.GoString() + fk.GoString()
	for _, secret := range secrets {
		if strings.Contains(printed, secret) {
			t.Error("formatted output holds key material")
		}
	}

	fk.Clear()
	if !allZero(dataKey) || fk.DataKey() != nil {
		t.Error("FileKey.Clear left the data key")
	}
	s.Clear()
	if !allZero(sessionKey) || s.Key() != nil {
		t.Error("Session.Clear left the sealing key")
	}
}

// TestClearDuringSealFile: a Clear that lands between SealFile calls never lets one wrap a
// data key under the zeroed sealing key; every key sealed before it unwraps under the real one.
func TestClearDuringSealFile(t *testing.T) {
	v, s, sessionKey := sealedKeyFixture(t)
	digest := sha256.Sum256([]byte("file"))

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		once  sync.Once
		ready = make(chan struct{})
		kept  []*FileKey
	)
	for range 8 {
		wg.Go(func() {
			for range 10000 {
				fk, err := s.SealFile(digest, 4)
				if err != nil {
					if !errors.Is(err, ErrSessionState) {
						t.Errorf("SealFile: %v", err)
					}
					return
				}
				once.Do(func() { close(ready) })
				mu.Lock()
				kept = append(kept, fk)
				mu.Unlock()
			}
		})
	}
	<-ready
	s.Clear()
	wg.Wait()

	for _, fk := range kept {
		if _, err := volumeformat.UnwrapDataKey(sessionKey, v.upload, fk.Identity(), fk.Sealed()); err != nil {
			t.Fatalf("a key sealed around Clear does not unwrap under the real sealing key: %v", err)
		}
	}
}
