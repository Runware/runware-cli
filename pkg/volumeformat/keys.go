package volumeformat

import (
	"crypto/hpke"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// Sizes on the wire.
const (
	PublicKeyBytes      = 1216                                          // X-Wing public key
	EncapsulationBytes  = 1120                                          // X-Wing encapsulation, the HPKE enc
	PrivateKeyBytes     = 32                                            // X-Wing seed
	SealedKeyBytes      = sealedKeyNonceBytes + dataKeyBytes + TagBytes // nonce, ciphertext, tag: 60
	sealedKeyNonceBytes = 12
)

func kem() hpke.KEM { return hpke.MLKEM768X25519() }

// SessionSealer is the CLI side of one upload session's key agreement: it holds the
// HPKE sender context from the encapsulation until the open response names the
// session.
type SessionSealer struct {
	enc    []byte
	sender *hpke.Sender
}

// Encapsulate makes the session's one X-Wing encapsulation to a key-pair public key,
// with randomness from crypto/rand. Session open carries Encapsulation().
//
// The caller must have verified the public key (its signed key-pair statement) before
// calling: HPKE base mode authenticates no sender, so anyone holding the public key
// can make an encapsulation and wrap a data key for any identity. A key that unwraps
// proves only that its sender held the session key; file integrity rests on the
// plaintext SHA-256 and size the reader checks against its trusted manifest.
func Encapsulate(publicKey []byte) (*SessionSealer, error) {
	pk, err := kem().NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("volume format: key-pair public key: %w", err)
	}
	enc, s, err := hpke.NewSender(pk, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
	if err != nil {
		return nil, fmt.Errorf("volume format: encapsulate: %w", err)
	}
	return &SessionSealer{
		enc:    enc,
		sender: s,
	}, nil
}

// Encapsulation is the 1120-byte HPKE enc sent at session open.
func (s *SessionSealer) Encapsulation() []byte { return append([]byte(nil), s.enc...) }

// SessionKey derives the 32-byte session sealing key once the open response names
// the session: an HPKE export with context fields(runware-volumes-v1/session-seal,
// organization, key pair, session, SHA-256(encapsulation)). The caller clears the
// returned key when the session ends.
func (s *SessionSealer) SessionKey(organization, keyPair, session [16]byte) ([]byte, error) {
	return s.sender.Export(sessionContext(organization, keyPair, session, s.enc), sessionKeyBytes)
}

// OpenSessionKey is the decrypting side: it decapsulates a session's encapsulation with
// the key pair's 32-byte private key and derives the same sealing key. X-Wing rejects
// implicitly, so a wrong encapsulation yields a key that later fails UnwrapDataKey.
// The caller clears the returned key once the session's data keys are unwrapped.
func OpenSessionKey(privateKey, enc []byte, organization, keyPair, session [16]byte) ([]byte, error) {
	sk, err := kem().NewPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("volume format: key-pair private key: %w", err)
	}
	r, err := hpke.NewRecipient(enc, sk, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: encapsulation: %w", ErrAuthentication, err)
	}
	return r.Export(sessionContext(organization, keyPair, session, enc), sessionKeyBytes)
}

func sessionContext(organization, keyPair, session [16]byte, enc []byte) string {
	h := sha256.Sum256(enc)
	var b []byte
	b = appendField(b, []byte(LabelSessionSeal))
	b = appendField(b, organization[:])
	b = appendField(b, keyPair[:])
	b = appendField(b, session[:])
	b = appendField(b, h[:])
	return string(b)
}

// NewDataKey returns a fresh 32-byte data key from crypto/rand. Draw one per file and
// attempt, never derive one from a hash or reuse it, and clear it after use.
func NewDataKey() []byte {
	k := make([]byte, dataKeyBytes)
	rand.Read(k) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	return k
}

// NewAttemptID returns a fresh random 128-bit attempt id from crypto/rand, drawn with
// each new data key.
func NewAttemptID() [16]byte {
	var id [16]byte
	rand.Read(id[:]) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	return id
}

// WrapDataKey seals a file's 32-byte data key under the session sealing key:
// AES-256-GCM with a random nonce from crypto/rand and associated data
// fields(runware-volumes-v1/data-key, organization, session, sha256, u64 size,
// attempt). The result is nonce(12) | ciphertext(32) | tag(16).
func WrapDataKey(sessionKey []byte, session [16]byte, id FileIdentity, dataKey []byte) ([]byte, error) {
	var n [sealedKeyNonceBytes]byte
	rand.Read(n[:]) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	return wrapDataKeyWithNonce(sessionKey, session, id, dataKey, n)
}

// wrapDataKeyWithNonce exists for the golden vectors, which fix the nonce.
func wrapDataKeyWithNonce(sessionKey []byte, session [16]byte, id FileIdentity, dataKey []byte, n [sealedKeyNonceBytes]byte) ([]byte, error) {
	if len(sessionKey) != sessionKeyBytes || len(dataKey) != dataKeyBytes {
		return nil, fmt.Errorf("volume format: session and data keys must be %d bytes", dataKeyBytes)
	}
	aead, err := newGCM(sessionKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, SealedKeyBytes)
	out = append(out, n[:]...)
	return aead.Seal(out, n[:], dataKey, dataKeyAAD(session, id)), nil
}

// UnwrapDataKey opens a sealed key, checking every bound field. Any mismatch returns
// ErrAuthentication and no key. The caller clears the returned key after use.
func UnwrapDataKey(sessionKey []byte, session [16]byte, id FileIdentity, sealed []byte) ([]byte, error) {
	if len(sealed) != SealedKeyBytes {
		return nil, fmt.Errorf("%w: sealed key is %d bytes, not %d", ErrAuthentication, len(sealed), SealedKeyBytes)
	}
	if len(sessionKey) != sessionKeyBytes {
		return nil, fmt.Errorf("volume format: session key must be %d bytes", sessionKeyBytes)
	}
	aead, err := newGCM(sessionKey)
	if err != nil {
		return nil, err
	}
	key, err := aead.Open(nil, sealed[:sealedKeyNonceBytes], sealed[sealedKeyNonceBytes:], dataKeyAAD(session, id))
	if err != nil {
		return nil, fmt.Errorf("%w: sealed key", ErrAuthentication)
	}
	return key, nil
}

func dataKeyAAD(session [16]byte, id FileIdentity) []byte {
	var b []byte
	b = appendField(b, []byte(LabelDataKey))
	b = appendField(b, id.Organization[:])
	b = appendField(b, session[:])
	b = appendField(b, id.SHA256[:])
	b = appendField(b, binary.BigEndian.AppendUint64(nil, id.Size))
	b = appendField(b, id.Attempt[:])
	return b
}
