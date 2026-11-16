// Package keypair verifies an organization's signed Volumes key-pair statement and seals
// per-file data keys to the key pair it names.
package keypair

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

const (
	statementLabel    = "runware-volumes-v1/key-pair-statement"
	lengthPrefixBytes = 4
	versionBytes      = 4
)

// Fields of an encoded statement, in order.
const (
	fieldLabel = iota
	fieldKEM
	fieldVersion
	fieldOrganization
	fieldKeyPair
	fieldSequence
	fieldIssuedAt
	fieldNotAfter
	fieldPublicKey
	statementFields
)

// ErrStatement is a key-pair statement the CLI refuses before sealing anything.
var ErrStatement = errors.New("volume key-pair statement refused")

type sizedField struct {
	index int
	name  string
	size  int
}

var sizedFields = []sizedField{
	{
		index: fieldVersion,
		name:  "signing key version",
		size:  versionBytes,
	},
	{
		index: fieldOrganization,
		name:  "organization",
		size:  16,
	},
	{
		index: fieldKeyPair,
		name:  "key pair",
		size:  16,
	},
	{
		index: fieldSequence,
		name:  "sequence",
		size:  8,
	},
	{
		index: fieldIssuedAt,
		name:  "issued-at",
		size:  8,
	},
	{
		index: fieldNotAfter,
		name:  "not-after",
		size:  8,
	},
	{
		index: fieldPublicKey,
		name:  "public key",
		size:  volumeformat.PublicKeyBytes,
	},
}

// Statement is a key-pair statement that Verify accepted; Open takes no other.
type Statement struct {
	SigningKeyVersion uint32
	Organization      [16]byte
	KeyPair           [16]byte
	Sequence          uint64
	IssuedAt          time.Time
	NotAfter          time.Time
	publicKey         []byte
}

// Verifier checks key-pair statements against the verification keys of one platform.
type Verifier struct {
	keys map[uint32]*ecdsa.PublicKey
	seq  sequenceStore
	now  func() time.Time
}

// NewVerifier returns the verifier for the platform at serverlessBaseURL, recording
// accepted sequences in configDir. A platform this release has no keys for refuses
// every statement.
func NewVerifier(serverlessBaseURL, configDir string) (*Verifier, error) {
	keys, err := parseKeys(selectKeys(verificationKeys, serverlessBaseURL))
	if err != nil {
		return nil, err
	}
	return NewVerifierWithKeys(keys, platformHost(serverlessBaseURL), configDir, time.Now)
}

// NewVerifierWithKeys is NewVerifier over explicit keys, platform host and clock, for tests in this module.
func NewVerifierWithKeys(keys map[uint32]*ecdsa.PublicKey, host, configDir string, now func() time.Time) (*Verifier, error) {
	if configDir == "" {
		return nil, errors.New("volume key-pair statement: the config directory is not set")
	}
	return &Verifier{
		keys: keys,
		seq:  sequenceStore{dir: configDir, host: host},
		now:  now,
	}, nil
}

// Verify accepts a statement a release key signed for the configured organization, before
// its not-after and no lower in sequence than any accepted before, and records its sequence.
func (v *Verifier) Verify(statement, signature []byte, organization [16]byte) (Statement, error) {
	if len(v.keys) == 0 {
		return Statement{}, refuse("this release has no verification keys for the platform")
	}
	fields, err := splitFields(statement)
	if err != nil {
		return Statement{}, err
	}
	if len(fields) <= fieldVersion || len(fields[fieldVersion]) != versionBytes {
		return Statement{}, refuse("signing key version is missing or not %d bytes", versionBytes)
	}
	version := binary.BigEndian.Uint32(fields[fieldVersion])
	key, ok := v.keys[version]
	if !ok {
		return Statement{}, refuse("unknown signing key version %d", version)
	}
	digest := sha256.Sum256(statement)
	if !ecdsa.VerifyASN1(key, digest[:], signature) {
		return Statement{}, refuse("signature does not verify")
	}
	st, err := parseStatement(fields)
	if err != nil {
		return Statement{}, err
	}
	if st.Organization != organization {
		return Statement{}, refuse("organization is not the configured one")
	}
	if !v.now().Before(st.NotAfter) {
		return Statement{}, refuse("statement is at or past its not-after")
	}
	if err := v.seq.accept(st.Organization, st.Sequence); err != nil {
		return Statement{}, err
	}
	return st, nil
}

func refuse(reason string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrStatement, fmt.Sprintf(reason, args...))
}

// splitFields cuts b into length-prefixed fields, stopping once there are more than a
// statement holds.
func splitFields(b []byte) ([][]byte, error) {
	var fields [][]byte
	for len(b) > 0 && len(fields) <= statementFields {
		if len(b) < lengthPrefixBytes {
			return nil, refuse("truncated field length")
		}
		n := binary.BigEndian.Uint32(b)
		if uint64(len(b)-lengthPrefixBytes) < uint64(n) {
			return nil, refuse("field %d runs past the end", len(fields)+1)
		}
		end := lengthPrefixBytes + int(n)
		fields = append(fields, b[lengthPrefixBytes:end])
		b = b[end:]
	}
	return fields, nil
}

func parseStatement(fields [][]byte) (Statement, error) {
	if len(fields) != statementFields {
		return Statement{}, refuse("field count is not %d", statementFields)
	}
	if string(fields[fieldLabel]) != statementLabel {
		return Statement{}, refuse("label is not the key-pair statement label")
	}
	if string(fields[fieldKEM]) != volumeformat.KEMIdentifier {
		return Statement{}, refuse("KEM identifier is not %s", volumeformat.KEMIdentifier)
	}
	for _, f := range sizedFields {
		if len(fields[f.index]) != f.size {
			return Statement{}, refuse("%s is %d bytes, want %d", f.name, len(fields[f.index]), f.size)
		}
	}
	st := Statement{
		SigningKeyVersion: binary.BigEndian.Uint32(fields[fieldVersion]),
		Sequence:          binary.BigEndian.Uint64(fields[fieldSequence]),
		IssuedAt:          time.Unix(int64(binary.BigEndian.Uint64(fields[fieldIssuedAt])), 0).UTC(),
		NotAfter:          time.Unix(int64(binary.BigEndian.Uint64(fields[fieldNotAfter])), 0).UTC(),
		publicKey:         bytes.Clone(fields[fieldPublicKey]),
	}
	copy(st.Organization[:], fields[fieldOrganization])
	copy(st.KeyPair[:], fields[fieldKeyPair])
	return st, nil
}
