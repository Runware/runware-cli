package volumeformat

import "errors"

// Sizes of the segmented construction and of the part layout.
const (
	SegmentBytes          = 1 << 20                 // one sealed segment, tag included
	TagBytes              = 16                      // AES-GCM tag
	SegmentPlaintextBytes = SegmentBytes - TagBytes // 1,048,560
	SaltBytes             = 16                      // random salt at the start of every part
	MinSegmentsPerPart    = 64                      // smallest part, about 64 MiB
	MaxParts              = 10_000                  // the object store's multipart limit
	MaxPartBytes          = int64(5) << 30          // the object store's largest part
	maxPlaintextBytes     = int64(1) << 62          // arithmetic bound, checked before any division
	segmentAADBytes       = 32 + 8 + 1              // binding, index, final flag
	dataKeyBytes          = 32                      // AES-256 key
	sessionKeyBytes       = 32                      // HPKE export length
)

// ErrLayout is a size, part or segment outside the part layout, or a layout that is
// not the one PlanLayout returns for its size.
var ErrLayout = errors.New("volume format: outside the part layout")

// Layout is the part layout of one file. The API returns it at declaration and the
// preparer reads it from the blob descriptor; both must equal PlanLayout(Size).
type Layout struct {
	Size            int64 // plaintext bytes
	Segments        int64 // sealed segments in the whole file, at least one
	SegmentsPerPart int64 // in every part but the last
	PartCount       int64
	PartSize        int64 // ciphertext bytes of every part but the last, salt included
	CiphertextSize  int64 // size + 16 per segment + 16 per part
}

// PlanLayout returns the layout for a plaintext size. A file has
// max(1, ceil(size / 1,048,560)) segments (an empty file is one segment holding only
// its tag), max(64, ceil(segments / 10,000)) of them per part so any file fits in
// 10,000 parts, and every part starts with its 16-byte salt. A size whose parts would
// exceed MaxPartBytes is refused, which caps a file at about 48 TiB of plaintext.
func PlanLayout(size int64) (Layout, error) {
	if size < 0 || size > maxPlaintextBytes {
		return Layout{}, ErrLayout
	}
	segments := max(1, ceilDiv(size, SegmentPlaintextBytes))
	perPart := max(MinSegmentsPerPart, ceilDiv(segments, MaxParts))
	if SaltBytes+perPart*SegmentBytes > MaxPartBytes {
		return Layout{}, ErrLayout
	}
	parts := ceilDiv(segments, perPart)
	return Layout{
		Size:            size,
		Segments:        segments,
		SegmentsPerPart: perPart,
		PartCount:       parts,
		PartSize:        SaltBytes + perPart*SegmentBytes,
		CiphertextSize:  size + TagBytes*segments + SaltBytes*parts,
	}, nil
}

// check refuses a layout PlanLayout would not return, so a hand-built or corrupted
// layout never reaches the cipher.
func (l Layout) check() error {
	want, err := PlanLayout(l.Size)
	if err != nil || want != l {
		return ErrLayout
	}
	return nil
}

// segmentRange is the first segment index (0-based, over the whole file) and the
// segment count of a 1-based part.
func (l Layout) segmentRange(part int64) (first, count int64, err error) {
	if err := l.check(); err != nil {
		return 0, 0, err
	}
	if part < 1 || part > l.PartCount {
		return 0, 0, ErrLayout
	}
	first = (part - 1) * l.SegmentsPerPart
	return first, min(l.SegmentsPerPart, l.Segments-first), nil
}

// SegmentsInPart is the number of sealed segments in a 1-based part.
func (l Layout) SegmentsInPart(part int64) (int64, error) {
	_, count, err := l.segmentRange(part)
	return count, err
}

// PlaintextRange is the offset and length of the plaintext a 1-based part seals.
func (l Layout) PlaintextRange(part int64) (offset, length int64, err error) {
	first, count, err := l.segmentRange(part)
	if err != nil {
		return 0, 0, err
	}
	offset = first * SegmentPlaintextBytes
	return offset, min(count*SegmentPlaintextBytes, l.Size-offset), nil
}

// PartLength is the exact ciphertext length of a 1-based part, salt included. The layout
// defines it, so every part of a file has a length known in advance.
func (l Layout) PartLength(part int64) (int64, error) {
	_, count, err := l.segmentRange(part)
	if err != nil {
		return 0, err
	}
	_, plain, _ := l.PlaintextRange(part) // cannot fail: segmentRange accepted the part
	return SaltBytes + plain + count*TagBytes, nil
}

// PartOffset is where a 1-based part starts in the stored object.
func (l Layout) PartOffset(part int64) (int64, error) {
	if _, _, err := l.segmentRange(part); err != nil {
		return 0, err
	}
	return (part - 1) * l.PartSize, nil
}

// SealedSegmentRange is the offset in the stored object and the length of sealed
// segment i (0-based within its 1-based part), for a ranged read that PartOpener then
// opens segment by segment.
func (l Layout) SealedSegmentRange(part, i int64) (offset, length int64, err error) {
	first, count, err := l.segmentRange(part)
	if err != nil {
		return 0, 0, err
	}
	if i < 0 || i >= count {
		return 0, 0, ErrLayout
	}
	offset = (part-1)*l.PartSize + SaltBytes + i*SegmentBytes
	return offset, l.segmentPlaintextLength(first+i) + TagBytes, nil
}

// segmentPlaintextLength is the plaintext length of a segment by its index over the
// whole file: full but for the last, which holds the rest (zero for an empty file).
func (l Layout) segmentPlaintextLength(global int64) int64 {
	return min(SegmentPlaintextBytes, l.Size-global*SegmentPlaintextBytes)
}

func ceilDiv(a, b int64) int64 { return (a + b - 1) / b }
