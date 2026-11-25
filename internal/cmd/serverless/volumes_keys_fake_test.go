package serverless

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/runware/runware-cli/internal/volumes/keypair"
	"github.com/runware/runware-cli/internal/volumes/put"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

// Statement fields the fake keys read, by position.
const (
	statementFields       = 9
	statementOrgField     = 3
	statementKeyPairField = 4
	statementNotAfter     = 7
	statementPublicKey    = 8
)

// fakeKeys stands in for the release verifier, which only the key-pair
// package can build over other keys. It refuses a statement the way the
// verifier does (signature, organization, not-after) and seals as
// keypair.Session does, with the same format functions.
type fakeKeys struct {
	signer *ecdsa.PublicKey
}

func refusedStatement(reason string) error {
	return fmt.Errorf("%w: %s", keypair.ErrStatement, reason)
}

func (k fakeKeys) Open(statement, signature []byte, organization [16]byte) (put.KeySession, error) {
	digest := sha256.Sum256(statement)
	if !ecdsa.VerifyASN1(k.signer, digest[:], signature) {
		return nil, refusedStatement("signature does not verify")
	}
	fields, ok := splitStatement(statement)
	if !ok || len(fields) != statementFields ||
		len(fields[statementOrgField]) != 16 || len(fields[statementKeyPairField]) != 16 || len(fields[statementNotAfter]) != 8 {
		return nil, refusedStatement("statement is malformed")
	}
	s := &fakeKeySession{}
	copy(s.org[:], fields[statementOrgField])
	copy(s.keyPair[:], fields[statementKeyPairField])
	if s.org != organization {
		return nil, refusedStatement("organization is not the configured one")
	}
	notAfter := time.Unix(int64(binary.BigEndian.Uint64(fields[statementNotAfter])), 0) //nolint:gosec // the fake wrote a positive time
	if !time.Now().Before(notAfter) {
		return nil, refusedStatement("statement is at or past its not-after")
	}
	sealer, err := volumeformat.Encapsulate(fields[statementPublicKey])
	if err != nil {
		return nil, err
	}
	s.sealer = sealer
	return s, nil
}

// splitStatement splits the length-prefixed fields the fake encodes.
func splitStatement(b []byte) ([][]byte, bool) {
	var fields [][]byte
	for len(b) > 0 {
		if len(b) < 4 {
			return nil, false
		}
		n := binary.BigEndian.Uint32(b)
		b = b[4:]
		if uint64(n) > uint64(len(b)) {
			return nil, false
		}
		fields = append(fields, b[:n])
		b = b[n:]
	}
	return fields, true
}

type fakeKeySession struct {
	mu      sync.Mutex
	org     [16]byte
	keyPair [16]byte
	sealer  *volumeformat.SessionSealer
	session [16]byte
	key     []byte
}

func (s *fakeKeySession) KeyPairID() [16]byte { return s.keyPair }

func (s *fakeKeySession) Encapsulation() []byte { return s.sealer.Encapsulation() }

func (s *fakeKeySession) Bind(session [16]byte) error {
	key, err := s.sealer.SessionKey(s.org, s.keyPair, session)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session, s.key = session, key
	return nil
}

func (s *fakeKeySession) Key() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key
}

func (s *fakeKeySession) SealFile(digest [32]byte, size uint64) (put.FileKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == nil {
		return nil, keypair.ErrSessionState
	}
	id := volumeformat.FileIdentity{
		Organization: s.org,
		SHA256:       digest,
		Size:         size,
		Attempt:      volumeformat.NewAttemptID(),
	}
	dataKey := volumeformat.NewDataKey()
	sealed, err := volumeformat.WrapDataKey(s.key, s.session, id, dataKey)
	if err != nil {
		return nil, err
	}
	return &fakeFileKey{
		dataKey:  dataKey,
		sealed:   sealed,
		identity: id,
	}, nil
}

func (s *fakeKeySession) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.key)
	s.key = nil
}

type fakeFileKey struct {
	dataKey  []byte
	sealed   []byte
	identity volumeformat.FileIdentity
}

func (f *fakeFileKey) DataKey() []byte { return f.dataKey }

func (f *fakeFileKey) Attempt() [16]byte { return f.identity.Attempt }

func (f *fakeFileKey) Sealed() []byte { return f.sealed }

func (f *fakeFileKey) Identity() volumeformat.FileIdentity { return f.identity }

func (f *fakeFileKey) Clear() { clear(f.dataKey) }
