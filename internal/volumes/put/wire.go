// Package put uploads local files to a Shared Volume.
package put

import (
	"log/slog"
	"time"
)

// States of an upload file, as the upload routes answer them.
const (
	stateUploading = "uploading"
	stateReused    = "reused"
	stateVerified  = "verified"
)

const redactedText = "[redacted]"

// Secret is a part token or an acknowledgement; it prints as [redacted].
type Secret string

func (Secret) String() string       { return redactedText }
func (Secret) GoString() string     { return redactedText }
func (Secret) LogValue() slog.Value { return slog.StringValue(redactedText) }

// KeyPairStatement is the organization's signed key-pair statement and its signature, unpadded base64url.
type KeyPairStatement struct {
	Statement string `json:"statement"`
	Signature string `json:"signature"`
}

// UploadOpen opens a session: the key pair id and the one encapsulation to its public key.
type UploadOpen struct {
	KeyPairID     string `json:"keyPairId"`
	Encapsulation string `json:"encapsulation"`
}

// Upload is an upload session with a fixed deadline, ExpiresAt.
type Upload struct {
	ID        string    `json:"id"`
	VolumeID  string    `json:"volumeId"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// PartLayout is the part layout the server approved for a file.
type PartLayout struct {
	SegmentBytes          int64 `json:"segmentBytes"`
	SegmentPlaintextBytes int64 `json:"segmentPlaintextBytes"`
	SegmentsPerPart       int64 `json:"segmentsPerPart"`
	PartSizeBytes         int64 `json:"partSizeBytes"`
	PartCount             int64 `json:"partCount"`
	CiphertextBytes       int64 `json:"ciphertextBytes"`
}

// UploadFile is one declared file of a session.
type UploadFile struct {
	ID        string      `json:"id"`
	Path      string      `json:"path"`
	Mode      uint32      `json:"mode"`
	Size      int64       `json:"size"`
	SHA256    string      `json:"sha256"`
	State     string      `json:"state"`
	AttemptID string      `json:"attemptId,omitempty"`
	Failure   string      `json:"failure,omitempty"`
	Layout    *PartLayout `json:"layout,omitempty"`
	CRC64NVME string      `json:"crc64nvme,omitempty"`
}

// FileDeclaration declares one file to add or replace, with its data key sealed.
type FileDeclaration struct {
	Path      string `json:"path"`
	Mode      uint32 `json:"mode"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	SealedKey string `json:"sealedKey"`
	AttemptID string `json:"attemptId"`
}

// FileDeclarations is a batch of declarations, at most maxDeclarationBatch files.
type FileDeclarations struct {
	Files []FileDeclaration `json:"files"`
}

// Target is where to send one ciphertext part and the token that authorizes it.
type Target struct {
	PartNumber int       `json:"partNumber"`
	URL        string    `json:"url"`
	Token      Secret    `json:"token"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// LogValue keeps the token and the address out of a log line.
func (t Target) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("partNumber", t.PartNumber),
		slog.Time("expiresAt", t.ExpiresAt),
	)
}

// BlobReference names content the organization already stores.
type BlobReference struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// DeclarationAnswer is the answer for one declared file: its layout and targets, or the blob it reuses.
type DeclarationAnswer struct {
	File          UploadFile     `json:"file"`
	UploadTargets []Target       `json:"uploadTargets,omitempty"`
	Blob          *BlobReference `json:"blob,omitempty"`
}

// DeclarationResult holds one answer per declared file, in request order.
type DeclarationResult struct {
	Files []DeclarationAnswer `json:"files"`
}

// PartTokenRequest asks for fresh targets for the named parts.
type PartTokenRequest struct {
	PartNumbers []int `json:"partNumbers"`
}

// UploadTargets answers a PartTokenRequest, in request order.
type UploadTargets struct {
	UploadTargets []Target `json:"uploadTargets"`
}

// PartAck is the acknowledgement the upload endpoint returned for one part.
type PartAck struct {
	PartNumber int    `json:"partNumber"`
	Ack        Secret `json:"ack"`
}

// LogValue keeps the acknowledgement out of a log line.
func (a PartAck) LogValue() slog.Value {
	return slog.GroupValue(slog.Int("partNumber", a.PartNumber))
}

// FileCompletion completes a file with the ciphertext CRC64/NVME and every part's ack.
type FileCompletion struct {
	CRC64NVME string    `json:"crc64nvme"`
	Parts     []PartAck `json:"parts"`
}

// UploadResult is what completing the session did to the Volume's latest files.
type UploadResult struct {
	Upload         Upload `json:"upload"`
	Changed        bool   `json:"changed"`
	ManifestSHA256 string `json:"manifestSha256,omitempty"`
}

// VolumeFile is one file of a Volume's latest files.
type VolumeFile struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// FileList is a Volume's latest files with the digest of the list.
type FileList struct {
	ManifestSHA256 string
	Files          []VolumeFile
}
