package volumeformat

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

// ErrAuthentication is a part, segment or key that fails to open: wrong key, wrong
// binding, or a swapped, reordered, truncated or extended segment. No plaintext of the
// failing segment is returned or written.
var ErrAuthentication = errors.New("volume format: authentication failed")

// ErrSealerUsed is a second sealing through one PartSealer. Every sealing needs a new
// sealer and so a fresh salt, or two plaintexts could meet under one key and nonce.
var ErrSealerUsed = errors.New("volume format: part sealer already used")

// FileIdentity is what every segment and the sealed data key are bound to. Environment,
// Volume and path are left out, so whole-file reuse within the organization works.
type FileIdentity struct {
	Organization [16]byte // UUID bytes, RFC 9562 order
	SHA256       [32]byte // plaintext SHA-256
	Size         uint64   // plaintext bytes
	Attempt      [16]byte // CLI-generated attempt id
}

// partCipher holds one part's key and the position of its segments in the file.
type partCipher struct {
	aead    cipher.AEAD
	layout  Layout
	part    int64
	first   int64 // index over the whole file of the part's first segment
	count   int64 // segments in the part
	binding [32]byte
}

// checkPart validates everything that keys a part except its salt: the layout, the
// part number, the identity's size and the data key's length. It returns the part's
// first segment (over the whole file) and its segment count.
func checkPart(dataKey []byte, id FileIdentity, l Layout, part int64) (first, count int64, err error) {
	first, count, err = l.segmentRange(part)
	if err != nil {
		return 0, 0, err
	}
	if id.Size != uint64(l.Size) { //nolint:gosec // l.Size is checked non-negative by segmentRange
		return 0, 0, ErrLayout
	}
	if len(dataKey) != dataKeyBytes {
		return 0, 0, fmt.Errorf("volume format: data key is %d bytes, not %d", len(dataKey), dataKeyBytes)
	}
	return first, count, nil
}

// newPartCipher keys a part. Every partCipher therefore holds a layout, part number
// and identity that checkPart accepted, which the methods below rely on.
func newPartCipher(dataKey []byte, id FileIdentity, l Layout, part int64, salt []byte) (*partCipher, error) {
	first, count, err := checkPart(dataKey, id, l, part)
	if err != nil {
		return nil, err
	}
	if len(salt) != SaltBytes {
		return nil, ErrLayout
	}
	key, err := partKey(dataKey, salt, part)
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(key)
	clear(key)
	if err != nil {
		return nil, err
	}
	return &partCipher{
		aead:    aead,
		layout:  l,
		part:    part,
		first:   first,
		count:   count,
		binding: segmentBinding(id),
	}, nil
}

// partKey is HKDF-SHA256 with the data key as input key, the part's salt as salt and
// info fields(runware-volumes-v1/part, u32 part number), the 1-based upload part
// number. A fresh salt gives a fresh key, so a part re-sealed from copied or
// rolled-back state never repeats a key and nonce pair.
func partKey(dataKey, salt []byte, part int64) ([]byte, error) {
	info := appendField(nil, []byte(LabelPart))
	info = appendField(info, binary.BigEndian.AppendUint32(nil, uint32(part))) //nolint:gosec // part is 1 to 10,000, checked by segmentRange
	return hkdf.Key(sha256.New, dataKey, salt, string(info), dataKeyBytes)
}

// segmentBinding hashes the file identity once, as every implementation does:
// SHA-256(fields(runware-volumes-v1/segment, organization, sha256, u64 size, attempt)).
func segmentBinding(id FileIdentity) [32]byte {
	var b []byte
	b = appendField(b, []byte(LabelSegment))
	b = appendField(b, id.Organization[:])
	b = appendField(b, id.SHA256[:])
	b = appendField(b, binary.BigEndian.AppendUint64(nil, id.Size))
	b = appendField(b, id.Attempt[:])
	return sha256.Sum256(b)
}

// segmentLength is the plaintext length of segment i of the part, or an error when
// the part has no segment i.
func (c *partCipher) segmentLength(i int64) (int64, error) {
	if i < 0 || i >= c.count {
		return 0, ErrLayout
	}
	return c.layout.segmentPlaintextLength(c.first + i), nil
}

// aad is the 41-byte segment associated data: the binding, the u64 segment index
// within the part, and a byte that is 1 only on the file's last segment.
func (c *partCipher) aad(dst []byte, i int64) []byte {
	dst = append(dst[:0], c.binding[:]...)
	dst = binary.BigEndian.AppendUint64(dst, uint64(i)) //nolint:gosec // i is a checked segment index
	if c.first+i == c.layout.Segments-1 {
		return append(dst, 1)
	}
	return append(dst, 0)
}

// nonce is four zero bytes and the u64 segment index within the part. The index
// restarts in every part; the part key differs, so no pair repeats.
func nonce(dst []byte, i int64) []byte {
	dst = append(dst[:0], 0, 0, 0, 0)
	return binary.BigEndian.AppendUint64(dst, uint64(i)) //nolint:gosec // i is a checked segment index
}

// PartSealer seals one plaintext of one 1-based part under a salt it draws itself.
// It is single-use: a resend of the part takes a new sealer and so a fresh salt, as
// resuming an upload requires. The single use is claimed atomically, so a sealer
// shared between goroutines still seals at most one plaintext. The flag sits behind a
// pointer, so a copy of a PartSealer value shares it with the original rather than
// starting unspent under the same salt.
type PartSealer struct {
	c    *partCipher
	salt [SaltBytes]byte
	used *atomic.Bool
}

// NewPartSealer keys a part with a fresh random salt from crypto/rand.
func NewPartSealer(dataKey []byte, id FileIdentity, l Layout, part int64) (*PartSealer, error) {
	var salt [SaltBytes]byte
	rand.Read(salt[:]) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	return newPartSealerWithSalt(dataKey, id, l, part, salt)
}

// newPartSealerWithSalt exists for the golden vectors, which fix the salt.
func newPartSealerWithSalt(dataKey []byte, id FileIdentity, l Layout, part int64, salt [SaltBytes]byte) (*PartSealer, error) {
	c, err := newPartCipher(dataKey, id, l, part, salt[:])
	if err != nil {
		return nil, err
	}
	return &PartSealer{
		c:    c,
		salt: salt,
		used: new(atomic.Bool),
	}, nil
}

// Salt is the part's salt, the first 16 bytes of the sealed part.
func (s *PartSealer) Salt() []byte { return append([]byte(nil), s.salt[:]...) }

// Len is the sealed part's exact length, salt included.
func (s *PartSealer) Len() int64 {
	n, _ := s.c.layout.PartLength(s.c.part) // cannot fail: newPartCipher checked the part
	return n
}

// start checks the plaintext against the part, then spends the sealer. A plaintext
// of the wrong length leaves the sealer unspent.
func (s *PartSealer) start(plaintext []byte) error {
	_, n, _ := s.c.layout.PlaintextRange(s.c.part) // cannot fail: newPartCipher checked the part
	if int64(len(plaintext)) != n {
		return ErrLayout
	}
	if !s.used.CompareAndSwap(false, true) {
		return ErrSealerUsed
	}
	return nil
}

// sealSegment appends sealed segment i of plaintext, the part's whole plaintext.
func (s *PartSealer) sealSegment(dst []byte, i int64, plaintext []byte, scratch *[segmentAADBytes + 12]byte) []byte {
	off := i * SegmentPlaintextBytes
	n, _ := s.c.segmentLength(i) // cannot fail: callers pass 0 <= i < count
	return s.c.aead.Seal(dst, nonce(scratch[segmentAADBytes:segmentAADBytes], i), plaintext[off:off+n], s.c.aad(scratch[:0], i))
}

// Seal returns the whole sealed part: the salt, then the part's sealed segments. The
// plaintext is the part's range of the file (Layout.PlaintextRange) and must not
// change while it is sealed.
func (s *PartSealer) Seal(plaintext []byte) ([]byte, error) {
	if err := s.start(plaintext); err != nil {
		return nil, err
	}
	out := make([]byte, 0, s.Len())
	out = append(out, s.salt[:]...)
	var scratch [segmentAADBytes + 12]byte
	for i := range s.c.count {
		out = s.sealSegment(out, i, plaintext, &scratch)
	}
	return out, nil
}

// Reader returns the sealed part as a stream of exactly Len bytes, sealing one
// segment at a time, so a part body costs one segment of memory beside the
// plaintext. The plaintext must not change until the reader is drained.
func (s *PartSealer) Reader(plaintext []byte) (io.Reader, error) {
	if err := s.start(plaintext); err != nil {
		return nil, err
	}
	buf := make([]byte, 0, SegmentBytes)
	return &sealedPartReader{
		s:       s,
		plain:   plaintext,
		pending: append(buf, s.salt[:]...),
		buf:     buf,
	}, nil
}

type sealedPartReader struct {
	s       *PartSealer
	plain   []byte
	pending []byte // sealed bytes not yet read
	buf     []byte // backing array reused for every segment
	next    int64  // next segment to seal
	scratch [segmentAADBytes + 12]byte
}

func (r *sealedPartReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if r.next == r.s.c.count {
			return 0, io.EOF
		}
		r.pending = r.s.sealSegment(r.buf[:0], r.next, r.plain, &r.scratch)
		r.next++
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// SealPart seals one 1-based part under a fresh random salt: NewPartSealer, then Seal.
func SealPart(dataKey []byte, id FileIdentity, l Layout, part int64, plaintext []byte) ([]byte, error) {
	s, err := NewPartSealer(dataKey, id, l, part)
	if err != nil {
		return nil, err
	}
	return s.Seal(plaintext)
}

// PartOpener opens the sealed segments of one 1-based part, in any order, so a
// reader can fetch a part in segment-aligned ranges (Layout.SealedSegmentRange).
// Its output is untrusted until the whole file's SHA-256 and size match the trusted
// manifest: anyone holding the key-pair public key can seal a data key and ciphertext
// for any identity, so segment authentication alone proves nothing about origin.
type PartOpener struct {
	c *partCipher
}

// NewPartOpener keys a part from its salt, the first 16 bytes of the sealed part.
func NewPartOpener(dataKey []byte, id FileIdentity, l Layout, part int64, salt []byte) (*PartOpener, error) {
	c, err := newPartCipher(dataKey, id, l, part, salt)
	if err != nil {
		return nil, err
	}
	return &PartOpener{c: c}, nil
}

// OpenSegment authenticates sealed segment i (0-based within the part) and appends its
// plaintext to dst. On any failure it returns ErrAuthentication and no plaintext.
func (o *PartOpener) OpenSegment(dst []byte, i int64, sealed []byte) ([]byte, error) {
	n, err := o.c.segmentLength(i)
	if err != nil {
		return nil, err
	}
	if int64(len(sealed)) != n+TagBytes {
		return nil, fmt.Errorf("%w: part %d segment %d is %d bytes, not %d", ErrAuthentication, o.c.part, i, len(sealed), n+TagBytes)
	}
	var scratch [segmentAADBytes + 12]byte
	out, err := o.c.aead.Open(dst, nonce(scratch[segmentAADBytes:segmentAADBytes], i), sealed, o.c.aad(scratch[:0], i))
	if err != nil {
		return nil, fmt.Errorf("%w: part %d segment %d", ErrAuthentication, o.c.part, i)
	}
	return out, nil
}

// OpenPart authenticates and decrypts a whole sealed part. It returns no plaintext
// unless every segment of the part authenticates.
func OpenPart(dataKey []byte, id FileIdentity, l Layout, part int64, sealed []byte) ([]byte, error) {
	want, err := l.PartLength(part)
	if err != nil {
		return nil, err
	}
	if int64(len(sealed)) != want {
		return nil, fmt.Errorf("%w: part %d is %d bytes, not %d", ErrAuthentication, part, len(sealed), want)
	}
	o, err := NewPartOpener(dataKey, id, l, part, sealed[:SaltBytes])
	if err != nil {
		return nil, err
	}
	body := sealed[SaltBytes:]
	plain := make([]byte, 0, int64(len(body))-o.c.count*TagBytes)
	for i := range o.c.count {
		n := int64(SegmentBytes)
		if i == o.c.count-1 {
			n = int64(len(body))
		}
		plain, err = o.OpenSegment(plain, i, body[:n])
		if err != nil {
			return nil, err
		}
		body = body[n:]
	}
	return plain, nil
}

// OpenPartTo streams one sealed part from r to w with one segment of memory. Each
// segment's plaintext is written only after that segment authenticates, and the
// part's last segment only after r is confirmed to end there, so no unauthenticated
// byte reaches w. A part that fails part-way has written the plaintext of its earlier,
// authentic segments: write to a staging file, discard it on any error (a retryable
// read error included, since a retry starts the part again), and publish it only on a
// nil error.
//
// A short or extended part returns ErrAuthentication; any other read error from r is
// returned as is, so the caller can retry the read. A layout, part, identity or data
// key that cannot open the part is refused before r is read.
func OpenPartTo(w io.Writer, r io.Reader, dataKey []byte, id FileIdentity, l Layout, part int64) (int64, error) {
	if _, _, err := checkPart(dataKey, id, l, part); err != nil {
		return 0, err
	}
	var salt [SaltBytes]byte
	if err := readExact(r, salt[:], part, -1); err != nil {
		return 0, err
	}
	o, err := NewPartOpener(dataKey, id, l, part, salt[:])
	if err != nil {
		return 0, err
	}
	sealed := make([]byte, SegmentBytes)
	plain := make([]byte, 0, SegmentPlaintextBytes)
	var written int64
	for i := range o.c.count {
		n, _ := o.c.segmentLength(i) // cannot fail: i < count
		seg := sealed[:n+TagBytes]
		if err := readExact(r, seg, part, i); err != nil {
			return written, err
		}
		plain, err = o.OpenSegment(plain[:0], i, seg)
		if err != nil {
			return written, err
		}
		if i == o.c.count-1 {
			if err := expectEOF(r, part); err != nil {
				return written, err
			}
		}
		m, err := w.Write(plain)
		written += int64(m)
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// readExact fills b from r. A stream that ends early is a truncated part.
func readExact(r io.Reader, b []byte, part, segment int64) error {
	_, err := io.ReadFull(r, b)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		if segment < 0 {
			return fmt.Errorf("%w: part %d ends inside its salt", ErrAuthentication, part)
		}
		return fmt.Errorf("%w: part %d ends inside segment %d", ErrAuthentication, part, segment)
	default:
		return err
	}
}

// expectEOF refuses bytes after a part's last segment.
func expectEOF(r io.Reader, part int64) error {
	var one [1]byte
	for range maxEmptyReads {
		n, err := r.Read(one[:])
		if n > 0 {
			return fmt.Errorf("%w: part %d continues after its last segment", ErrAuthentication, part)
		}
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return err
		}
	}
	return io.ErrNoProgress
}

// maxEmptyReads bounds how often a reader may return nothing and no error, as
// bufio does, before the stream counts as broken.
const maxEmptyReads = 100

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
