// Package volumeformat is the Shared Volumes encrypted file format: the part layout,
// the segmented AES-256-GCM construction with a random salt per part, the session
// sealing key agreed over X-Wing, and the data-key wrap. The CLI seals files with it
// and the decrypting side opens them, so it depends on the standard library only and
// lives outside internal/ where any consumer of this package can import it.
//
// testdata/format-vectors.json holds the golden vectors every implementation must
// reproduce byte for byte.
//
// Salts, the wrap nonce and the encapsulation are drawn from crypto/rand inside the
// package, so no caller can reuse a key and nonce pair by passing the same salt twice.
// The data key and attempt id are the caller's: draw them with NewDataKey and
// NewAttemptID, one fresh data key per file and attempt, never derived or reused.
//
// Keys returned by this package (data keys, session sealing keys) are plain byte
// slices: clear() each one as soon as it is no longer needed, and drop sealers,
// openers and session sealers promptly. Go cannot zero the AES key schedules they
// hold, so their lifetime is the caller's to keep short.
package volumeformat

import "encoding/binary"

// Labels give every derivation and associated-data string its own domain.
const (
	LabelSessionSeal = "runware-volumes-v1/session-seal"
	LabelDataKey     = "runware-volumes-v1/data-key"
	LabelPart        = "runware-volumes-v1/part"
	LabelSegment     = "runware-volumes-v1/segment"
)

// KEMIdentifier names the KEM a key-pair statement's public key belongs to:
// MLKEM768-X25519 (X-Wing), HPKE KEM 0x647A of draft-ietf-hpke-pq, as Go's
// crypto/hpke implements it.
const KEMIdentifier = "hpke-kem-0x647a"

// appendField appends b prefixed by its length as a 4-byte big-endian integer. Every
// multi-field byte string of the format (exporter context, associated data, HKDF
// info) is a sequence of such fields, so no two inputs encode alike.
func appendField(dst, b []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(b))) //nolint:gosec // every field is a label, id, digest or integer, far below 4 GiB
	return append(dst, b...)
}
