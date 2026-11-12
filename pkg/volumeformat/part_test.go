package volumeformat

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/iotest"
)

// layoutCase is a plaintext size and the layout PlanLayout must return for it.
type layoutCase struct {
	name       string
	size       int64
	segments   int64
	perPart    int64
	partCount  int64
	ciphertext int64
}

// TestPlanLayout matches the part-layout check the API enforces
// (volume_upload_files_part_layout_check): parts of 16 + k * 1 MiB holding
// ceil(segments / k) parts, and a ciphertext of size + 16 per segment + 16 per part.
func TestPlanLayout(t *testing.T) {
	const (
		mib  = 1 << 20
		seg  = SegmentPlaintextBytes
		salt = SaltBytes
	)
	tests := []layoutCase{
		{
			name:       "empty file",
			size:       0,
			segments:   1,
			perPart:    64,
			partCount:  1,
			ciphertext: 32,
		},
		{
			name:       "one byte",
			size:       1,
			segments:   1,
			perPart:    64,
			partCount:  1,
			ciphertext: 33,
		},
		{
			name:       "exactly 64 segments",
			size:       64 * seg,
			segments:   64,
			perPart:    64,
			partCount:  1,
			ciphertext: 64*mib + salt,
		},
		{
			name:       "one byte over 64 segments",
			size:       64*seg + 1,
			segments:   65,
			perPart:    64,
			partCount:  2,
			ciphertext: 64*seg + 1 + 65*16 + 2*salt,
		},
		{
			name:       "64 MiB",
			size:       64 * mib,
			segments:   65,
			perPart:    64,
			partCount:  2,
			ciphertext: 64*mib + 65*16 + 2*salt,
		},
		{
			name:       "250 GiB",
			size:       250 << 30,
			segments:   256004,
			perPart:    64,
			partCount:  4001,
			ciphertext: 250<<30 + 256004*16 + 4001*salt,
		},
		{
			name:       "exactly 10,000 parts of 64",
			size:       640000 * seg,
			segments:   640000,
			perPart:    64,
			partCount:  10000,
			ciphertext: 640000*mib + 10000*salt,
		},
		{
			name:       "one segment over 10,000 parts of 64",
			size:       640000*seg + 1,
			segments:   640001,
			perPart:    65,
			partCount:  9847,
			ciphertext: 640000*seg + 1 + 640001*16 + 9847*salt,
		},
		{
			name:       "1 TiB",
			size:       1 << 40,
			segments:   1048593,
			perPart:    105,
			partCount:  9987,
			ciphertext: 1<<40 + 1048593*16 + 9987*salt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := PlanLayout(tt.size)
			if err != nil {
				t.Fatal(err)
			}
			if l.Segments != tt.segments || l.SegmentsPerPart != tt.perPart || l.PartCount != tt.partCount || l.CiphertextSize != tt.ciphertext {
				t.Fatalf("got %+v", l)
			}
			if l.PartSize != salt+tt.perPart*mib || l.PartCount > MaxParts {
				t.Fatalf("part size %d, %d parts", l.PartSize, l.PartCount)
			}
			// The parts tile the plaintext and the ciphertext exactly, no part empty.
			var plain, sealed int64
			for p := int64(1); p <= l.PartCount; p++ {
				off, n, _ := l.PlaintextRange(p)
				length, _ := l.PartLength(p)
				start, _ := l.PartOffset(p)
				if off != plain || start != sealed || length <= SaltBytes || (p < l.PartCount && length != l.PartSize) {
					t.Fatalf("part %d: plaintext %d+%d, sealed %d+%d", p, off, n, start, length)
				}
				plain += n
				sealed += length
				if l.PartCount > 3 && p == 2 {
					p = l.PartCount - 2 // skip the identical middle parts
					plain, sealed = (l.PartCount-2)*l.SegmentsPerPart*seg, (l.PartCount-2)*l.PartSize
				}
			}
			if plain != tt.size || sealed != tt.ciphertext {
				t.Fatalf("parts cover %d plaintext and %d sealed bytes", plain, sealed)
			}
		})
	}
	// The largest file whose parts fit MaxPartBytes: 5,119 segments per part.
	const largest = 5119 * MaxParts * seg
	l, err := PlanLayout(largest)
	if err != nil || l.SegmentsPerPart != 5119 || l.PartCount != MaxParts || l.PartSize > MaxPartBytes {
		t.Fatalf("largest file: %+v, %v", l, err)
	}
	for _, size := range []int64{-1, largest + 1, maxPlaintextBytes, maxPlaintextBytes + 1} {
		if _, err := PlanLayout(size); !errors.Is(err, ErrLayout) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
	l, _ = PlanLayout(100)
	for _, p := range []int64{0, 2} {
		if _, err := l.PartLength(p); !errors.Is(err, ErrLayout) {
			t.Fatalf("part %d: %v", p, err)
		}
	}
}

// TestLayoutIsChecked: a layout that PlanLayout would not return for its size never
// keys a part, so a corrupted descriptor cannot move segment boundaries.
func TestLayoutIsChecked(t *testing.T) {
	f := newFixture(t, 3*SegmentPlaintextBytes)
	bad := f.layout
	bad.SegmentsPerPart = 1
	if _, err := NewPartSealer(f.key, f.id, bad, 1); !errors.Is(err, ErrLayout) {
		t.Fatalf("hand-built layout: %v", err)
	}
	other := f.id
	other.Size++
	if _, err := NewPartSealer(f.key, other, f.layout, 1); !errors.Is(err, ErrLayout) {
		t.Fatalf("identity of another size: %v", err)
	}
	if _, err := NewPartSealer(f.key[:16], f.id, f.layout, 1); err == nil {
		t.Fatal("a 16-byte data key keyed a part")
	}
}

// TestSealOpenRoundTrip seals parts of several shapes whole and streamed, and opens
// them whole, streamed and by segment-aligned ranges.
func TestSealOpenRoundTrip(t *testing.T) {
	for _, size := range []int64{0, 1, SegmentPlaintextBytes - 1, SegmentPlaintextBytes, SegmentPlaintextBytes + 1, 3*SegmentPlaintextBytes + 7} {
		f := newFixture(t, size)
		s, err := NewPartSealer(f.key, f.id, f.layout, 1)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.Reader(f.plain)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(sealed)) != s.Len() || int64(len(sealed)) != f.layout.CiphertextSize || !bytes.Equal(sealed[:SaltBytes], s.Salt()) {
			t.Fatalf("size %d: sealed %d bytes, Len %d", size, len(sealed), s.Len())
		}

		plain, err := OpenPart(f.key, f.id, f.layout, 1, sealed)
		if err != nil || !bytes.Equal(plain, f.plain) {
			t.Fatalf("size %d: OpenPart: %v", size, err)
		}
		var w bytes.Buffer
		if n, err := OpenPartTo(&w, iotest.OneByteReader(bytes.NewReader(sealed)), f.key, f.id, f.layout, 1); err != nil || n != size || !bytes.Equal(w.Bytes(), f.plain) {
			t.Fatalf("size %d: OpenPartTo: %d, %v", size, n, err)
		}

		o, err := NewPartOpener(f.key, f.id, f.layout, 1, sealed[:SaltBytes])
		if err != nil {
			t.Fatal(err)
		}
		count, _ := f.layout.SegmentsInPart(1)
		var ranged []byte
		for i := count - 1; i >= 0; i-- { // any order
			off, n, err := f.layout.SealedSegmentRange(1, i)
			if err != nil {
				t.Fatal(err)
			}
			seg, err := o.OpenSegment(nil, i, sealed[off:off+n])
			if err != nil {
				t.Fatalf("size %d segment %d: %v", size, i, err)
			}
			ranged = append(seg, ranged...)
		}
		if !bytes.Equal(ranged, f.plain) {
			t.Fatalf("size %d: ranged open differs", size)
		}
		if _, _, err := f.layout.SealedSegmentRange(1, count); !errors.Is(err, ErrLayout) {
			t.Fatalf("segment past the part: %v", err)
		}
	}
}

// TestSealerIsSingleUse: a sealer seals one plaintext once, so two plaintexts never
// meet under one salt.
// TestSealerIsSingleUseAcrossGoroutines: two goroutines racing on one sealer, as a
// retry racing its original attempt might, seal at most one plaintext between them.
func TestSealerIsSingleUseAcrossGoroutines(t *testing.T) {
	f := newFixture(t, 10)
	s, err := NewPartSealer(f.key, f.id, f.layout, 1)
	if err != nil {
		t.Fatal(err)
	}
	other := bytes.Clone(f.plain)
	other[0] ^= 1
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, plain := range [][]byte{f.plain, other} {
		wg.Go(func() {
			_, err := s.Seal(plain)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	var sealed, refused int
	for err := range errs {
		switch {
		case err == nil:
			sealed++
		case errors.Is(err, ErrSealerUsed):
			refused++
		default:
			t.Fatal(err)
		}
	}
	if sealed != 1 || refused != 1 {
		t.Fatalf("%d seals and %d refusals, want one of each", sealed, refused)
	}
}

func TestSealerIsSingleUse(t *testing.T) {
	f := newFixture(t, 10)
	s, err := NewPartSealer(f.key, f.id, f.layout, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seal(make([]byte, 9)); !errors.Is(err, ErrLayout) {
		t.Fatalf("short plaintext: %v", err)
	}
	if _, err := s.Seal(f.plain); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seal(f.plain); !errors.Is(err, ErrSealerUsed) {
		t.Fatalf("second Seal: %v", err)
	}
	if _, err := s.Reader(f.plain); !errors.Is(err, ErrSealerUsed) {
		t.Fatalf("Reader after Seal: %v", err)
	}
}

// TestResumeFromCopiedStateNeverRepeatsAKey is the resume test:
// sealing the same part again from the same saved state (data key, attempt, layout)
// draws a fresh salt, so the part key differs and no key and nonce pair repeats,
// while each copy still opens. Segment indexes restart per part, and the part number
// in the key keeps parts apart under one salt too.
func TestResumeFromCopiedStateNeverRepeatsAKey(t *testing.T) {
	f := newFixture(t, 2*SegmentPlaintextBytes)
	keys := map[[32]byte]struct{}{}
	for range 64 {
		sealed, err := SealPart(f.key, f.id, f.layout, 1, f.plain)
		if err != nil {
			t.Fatal(err)
		}
		k, err := partKey(f.key, sealed[:SaltBytes], 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := keys[[32]byte(k)]; dup {
			t.Fatal("a re-sealed part repeated its key")
		}
		keys[[32]byte(k)] = struct{}{}
		if plain, err := OpenPart(f.key, f.id, f.layout, 1, sealed); err != nil || !bytes.Equal(plain, f.plain) {
			t.Fatalf("re-sealed copy does not open: %v", err)
		}
	}
	salt := make([]byte, SaltBytes)
	k1, _ := partKey(f.key, salt, 1)
	k2, _ := partKey(f.key, salt, 2)
	if bytes.Equal(k1, k2) {
		t.Fatal("two parts share a key under one salt")
	}
}

// tamperCase is a data key and the bytes opened as part 1 with it.
type tamperCase struct {
	key  []byte
	data []byte
}

// TestTamperingFailsBeforePlaintext covers what the vectors do not: a segment moved
// from another file, two full segments swapped, a part cut at a segment boundary
// before its final segment, a wrong data key, and a stream that breaks with an I/O
// error, which is returned as is.
func TestTamperingFailsBeforePlaintext(t *testing.T) {
	f := newFixture(t, 3*SegmentPlaintextBytes+7)
	sealed, err := SealPart(f.key, f.id, f.layout, 1, f.plain)
	if err != nil {
		t.Fatal(err)
	}
	other := newFixture(t, 3*SegmentPlaintextBytes+7)
	otherSealed, err := SealPart(other.key, other.id, other.layout, 1, other.plain)
	if err != nil {
		t.Fatal(err)
	}
	seg := func(b []byte, i int) []byte { return b[SaltBytes+i*SegmentBytes : SaltBytes+(i+1)*SegmentBytes] }
	wrongKey := bytes.Clone(f.key)
	wrongKey[0] ^= 1
	cases := map[string]tamperCase{
		"segment from another file": {
			key:  f.key,
			data: concat(sealed[:SaltBytes+SegmentBytes], seg(otherSealed, 1), sealed[SaltBytes+2*SegmentBytes:]),
		},
		"segments 1 and 2 swapped": {
			key:  f.key,
			data: concat(sealed[:SaltBytes], seg(sealed, 1), seg(sealed, 0), sealed[SaltBytes+2*SegmentBytes:]),
		},
		"cut before the final segment": {
			key:  f.key,
			data: sealed[:SaltBytes+3*SegmentBytes],
		},
		"wrong data key": {
			key:  wrongKey,
			data: sealed,
		},
		"empty": {
			key:  f.key,
			data: nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if plain, err := OpenPart(tc.key, f.id, f.layout, 1, tc.data); !errors.Is(err, ErrAuthentication) || plain != nil {
				t.Fatalf("OpenPart: %v", err)
			}
			var w bytes.Buffer
			_, err := OpenPartTo(&w, bytes.NewReader(tc.data), tc.key, f.id, f.layout, 1)
			if !errors.Is(err, ErrAuthentication) || !bytes.HasPrefix(f.plain, w.Bytes()) {
				t.Fatalf("OpenPartTo: %v after %d bytes", err, w.Len())
			}
		})
	}

	errBroken := errors.New("connection reset")
	r := io.MultiReader(bytes.NewReader(sealed[:SaltBytes+10]), errReader{
		err: errBroken,
	})
	if _, err := OpenPartTo(io.Discard, r, f.key, f.id, f.layout, 1); !errors.Is(err, errBroken) || errors.Is(err, ErrAuthentication) {
		t.Fatalf("I/O error: %v", err)
	}
}

// TestDataKeyWrap: a wrapped key opens only under the identity and session it was
// wrapped for, and two wraps of one key differ.
func TestDataKeyWrap(t *testing.T) {
	f := newFixture(t, 5)
	sessionKey := make([]byte, 32)
	rand.Read(sessionKey) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	var session [16]byte
	rand.Read(session[:]) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	a, err := WrapDataKey(sessionKey, session, f.id, f.key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := WrapDataKey(sessionKey, session, f.id, f.key)
	if len(a) != SealedKeyBytes || bytes.Equal(a, b) {
		t.Fatalf("sealed key %d bytes, repeated nonce %v", len(a), bytes.Equal(a, b))
	}
	if k, err := UnwrapDataKey(sessionKey, session, f.id, a); err != nil || !bytes.Equal(k, f.key) {
		t.Fatalf("unwrap: %v", err)
	}
	changes := map[string]func(*FileIdentity, *[16]byte){
		"organization": func(id *FileIdentity, _ *[16]byte) { id.Organization[0] ^= 1 },
		"sha256":       func(id *FileIdentity, _ *[16]byte) { id.SHA256[0] ^= 1 },
		"size":         func(id *FileIdentity, _ *[16]byte) { id.Size++ },
		"attempt":      func(id *FileIdentity, _ *[16]byte) { id.Attempt[0] ^= 1 },
		"session":      func(_ *FileIdentity, s *[16]byte) { s[0] ^= 1 },
	}
	for name, change := range changes {
		id, s := f.id, session
		change(&id, &s)
		if _, err := UnwrapDataKey(sessionKey, s, id, a); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("changed %s: %v", name, err)
		}
	}
	if _, err := UnwrapDataKey(sessionKey, session, f.id, a[:59]); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("short sealed key: %v", err)
	}
}

// TestOpenErrorPaths: a salt of the wrong length, a segment index outside the part, a
// sealed segment of the wrong length, a bad part number, a writer that fails, and a
// read error after the last segment are each refused.
func TestOpenErrorPaths(t *testing.T) {
	f := newFixture(t, 10)
	sealed, err := SealPart(f.key, f.id, f.layout, 1, f.plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPartOpener(f.key, f.id, f.layout, 1, sealed[:SaltBytes-1]); !errors.Is(err, ErrLayout) {
		t.Fatalf("short salt: %v", err)
	}
	o, err := NewPartOpener(f.key, f.id, f.layout, 1, sealed[:SaltBytes])
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int64{-1, 1} { // the part has one segment
		if _, err := o.OpenSegment(nil, i, sealed[SaltBytes:]); !errors.Is(err, ErrLayout) {
			t.Fatalf("segment %d: %v", i, err)
		}
	}
	if _, err := o.OpenSegment(nil, 0, sealed[SaltBytes:len(sealed)-1]); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("short segment: %v", err)
	}
	if _, _, err := f.layout.SealedSegmentRange(1, -1); !errors.Is(err, ErrLayout) {
		t.Fatalf("range of segment -1: %v", err)
	}
	// A bad part number is refused before the reader is touched, so a caller can
	// retry on the same reader.
	untouched := bytes.NewReader(sealed)
	if _, err := OpenPartTo(io.Discard, untouched, f.key, f.id, f.layout, 0); !errors.Is(err, ErrLayout) || untouched.Len() != len(sealed) {
		t.Fatalf("part 0: %v, %d of %d bytes left", err, untouched.Len(), len(sealed))
	}
	errWrite := errors.New("disk full")
	if _, err := OpenPartTo(errWriter{
		err: errWrite,
	}, bytes.NewReader(sealed), f.key, f.id, f.layout, 1); !errors.Is(err, errWrite) {
		t.Fatalf("write error: %v", err)
	}
	errBroken := errors.New("connection reset")
	var w bytes.Buffer
	r := io.MultiReader(bytes.NewReader(sealed), errReader{
		err: errBroken,
	})
	if _, err := OpenPartTo(&w, r, f.key, f.id, f.layout, 1); !errors.Is(err, errBroken) || w.Len() != 0 {
		t.Fatalf("read error after the last segment: %v, %d bytes written", err, w.Len())
	}
}

func TestCRC64NVMECheckValue(t *testing.T) {
	h := NewCRC64NVME()
	_, _ = h.Write([]byte("123456789")) // a hash never fails a write
	if got := h.Sum64(); got != 0xae8b14860a799888 {
		t.Fatalf("CRC-64/NVME check value %x", got)
	}
}

// BenchmarkSealPart and BenchmarkOpenPartTo measure one full 64-segment part, the
// part size of every file up to 640,000 segments (about 610 GiB).
func BenchmarkSealPart(b *testing.B) {
	f := newFixture(b, MinSegmentsPerPart*SegmentPlaintextBytes)
	b.SetBytes(f.layout.Size)
	for b.Loop() {
		s, err := NewPartSealer(f.key, f.id, f.layout, 1)
		if err != nil {
			b.Fatal(err)
		}
		r, err := s.Reader(f.plain)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenPartTo(b *testing.B) {
	f := newFixture(b, MinSegmentsPerPart*SegmentPlaintextBytes)
	sealed, err := SealPart(f.key, f.id, f.layout, 1, f.plain)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(f.layout.Size)
	for b.Loop() {
		if _, err := OpenPartTo(io.Discard, bytes.NewReader(sealed), f.key, f.id, f.layout, 1); err != nil {
			b.Fatal(err)
		}
	}
}

type fixture struct {
	plain  []byte
	key    []byte
	id     FileIdentity
	layout Layout
}

func newFixture(tb testing.TB, size int64) fixture {
	tb.Helper()
	plain := make([]byte, size)
	rand.Read(plain) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	key := make([]byte, 32)
	rand.Read(key) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	id := FileIdentity{
		SHA256: sha256.Sum256(plain),
		Size:   uint64(size),
	}
	rand.Read(id.Organization[:]) //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	rand.Read(id.Attempt[:])      //nolint:errcheck,gosec // crypto/rand.Read never returns an error
	l, err := PlanLayout(size)
	if err != nil {
		tb.Fatal(err)
	}
	return fixture{
		plain:  plain,
		key:    key,
		id:     id,
		layout: l,
	}
}

type errReader struct{ err error }

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
