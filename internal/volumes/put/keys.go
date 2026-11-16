package put

import (
	"github.com/runware/runware-cli/internal/volumes/keypair"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

// Keys verifies the key-pair statement and opens a session's key agreement.
type Keys interface {
	Open(statement, signature []byte, organization [16]byte) (KeySession, error)
}

// KeySession is one upload session's key agreement and its per-file data keys.
type KeySession interface {
	KeyPairID() [16]byte
	Encapsulation() []byte
	Bind(session [16]byte) error
	Key() []byte
	SealFile(digest [32]byte, size uint64) (FileKey, error)
	Clear()
}

// FileKey is one file attempt's data key with its sealed form and identity.
type FileKey interface {
	DataKey() []byte
	Attempt() [16]byte
	Sealed() []byte
	Identity() volumeformat.FileIdentity
	Clear()
}

var (
	_ FileKey    = (*keypair.FileKey)(nil)
	_ KeySession = keypairSession{}
)

// NewKeys returns the Keys that verify statements with v.
func NewKeys(v *keypair.Verifier) Keys { return keypairKeys{verifier: v} }

type keypairKeys struct {
	verifier *keypair.Verifier
}

func (k keypairKeys) Open(statement, signature []byte, organization [16]byte) (KeySession, error) {
	st, err := k.verifier.Verify(statement, signature, organization)
	if err != nil {
		return nil, err
	}
	s, err := keypair.Open(st)
	if err != nil {
		return nil, err
	}
	return keypairSession{
		Session: s,
		keyPair: st.KeyPair,
	}, nil
}

type keypairSession struct {
	*keypair.Session
	keyPair [16]byte
}

func (s keypairSession) KeyPairID() [16]byte { return s.keyPair }

func (s keypairSession) SealFile(digest [32]byte, size uint64) (FileKey, error) {
	fk, err := s.Session.SealFile(digest, size)
	if err != nil {
		return nil, err
	}
	return fk, nil
}
