package keypair

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

const redactedText = "[redacted]"

// ErrSessionState is a Bind or SealFile call a session is not open for: sealing before Bind,
// binding twice, or either after Clear.
var ErrSessionState = errors.New("volume key session is not in a state for this call")

// redacting makes a type that embeds it print as [redacted] in slog and under every fmt verb, but fmt skips it in an unexported field (see secret).
type redacting struct{}

func (redacting) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redactedText) }
func (redacting) String() string             { return redactedText }
func (redacting) GoString() string           { return redactedText }
func (redacting) LogValue() slog.Value       { return slog.StringValue(redactedText) }

// secret holds key material behind a func, which fmt prints as an address under every verb, so a
// struct holding a Session or FileKey in an unexported field prints no key.
type secret func() []byte

func newSecret(b []byte) secret { return func() []byte { return b } }

func (k secret) reveal() []byte {
	if k == nil {
		return nil
	}
	return k()
}

// Session is one upload session's key agreement: one encapsulation to the verified key
// pair, then a sealing key and a data key per file attempt.
type Session struct {
	redacting

	mu      sync.Mutex
	org     [16]byte
	keyPair [16]byte
	enc     []byte
	sealer  *volumeformat.SessionSealer
	session [16]byte
	key     secret
}

// Open makes the session's one encapsulation to a verified statement's key pair.
func Open(st Statement) (*Session, error) {
	if len(st.publicKey) != volumeformat.PublicKeyBytes {
		return nil, refuse("statement was not verified")
	}
	sealer, err := volumeformat.Encapsulate(st.publicKey)
	if err != nil {
		return nil, fmt.Errorf("encapsulate to the key pair: %w", err)
	}
	return &Session{
		org:     st.Organization,
		keyPair: st.KeyPair,
		enc:     sealer.Encapsulation(),
		sealer:  sealer,
	}, nil
}

// Encapsulation is the encapsulation that session open carries.
func (s *Session) Encapsulation() []byte { return bytes.Clone(s.enc) }

// Bind derives the session sealing key once the open response names the session.
func (s *Session) Bind(session [16]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealer == nil {
		return ErrSessionState
	}
	key, err := s.sealer.SessionKey(s.org, s.keyPair, session)
	if err != nil {
		return fmt.Errorf("derive the session sealing key: %w", err)
	}
	s.session, s.key, s.sealer = session, newSecret(key), nil
	return nil
}

// Key is the session sealing key for the private resume journal; callers must not log or
// keep it. It is the live slice, so Clear zeroes it.
func (s *Session) Key() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key.reveal()
}

// SealFile draws a fresh data key and attempt id for one file attempt and seals the key.
func (s *Session) SealFile(digest [32]byte, size uint64) (*FileKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == nil {
		return nil, ErrSessionState
	}
	id := volumeformat.FileIdentity{
		Organization: s.org,
		SHA256:       digest,
		Size:         size,
		Attempt:      volumeformat.NewAttemptID(),
	}
	dataKey := volumeformat.NewDataKey()
	sealed, err := volumeformat.WrapDataKey(s.key.reveal(), s.session, id, dataKey)
	if err != nil {
		clear(dataKey)
		return nil, fmt.Errorf("seal the file's data key: %w", err)
	}
	return &FileKey{
		dataKey:  newSecret(dataKey),
		sealed:   sealed,
		identity: id,
	}, nil
}

// Clear zeroes the sealing key and drops the sealer; it does not clear FileKeys, which callers clear with FileKey.Clear.
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.key.reveal())
	s.key, s.sealer = nil, nil
}

// FileKey is one file attempt's data key with its sealed form and identity.
type FileKey struct {
	redacting

	dataKey  secret
	sealed   []byte
	identity volumeformat.FileIdentity
}

// DataKey is the live data key for volumeformat.NewPartSealer; Clear zeroes it.
func (f *FileKey) DataKey() []byte { return f.dataKey.reveal() }

// Attempt is the random attempt id sent with the sealed key.
func (f *FileKey) Attempt() [16]byte { return f.identity.Attempt }

// Sealed is the 60-byte sealed key: nonce, ciphertext, tag.
func (f *FileKey) Sealed() []byte { return f.sealed }

// Identity is what every segment and the sealed key are bound to.
func (f *FileKey) Identity() volumeformat.FileIdentity { return f.identity }

// Clear zeroes the data key.
func (f *FileKey) Clear() {
	clear(f.dataKey.reveal())
	f.dataKey = nil
}
