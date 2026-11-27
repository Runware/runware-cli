package volumeformat

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"runtime"
	"testing"
	"testing/iotest"
)

// TestStreamReaderMatchesSeal: the same part sealed under the same salt by Seal, by
// Reader and by StreamReader gives the same bytes, however the plaintext source and the
// consumer split their reads.
func TestStreamReaderMatchesSeal(t *testing.T) {
	sizes := []int64{
		0,
		1,
		SegmentPlaintextBytes - 1,
		SegmentPlaintextBytes,
		SegmentPlaintextBytes + 1,
		3*SegmentPlaintextBytes + 7,
		MinSegmentsPerPart * SegmentPlaintextBytes,
		MinSegmentsPerPart*SegmentPlaintextBytes + 1,
	}
	for _, size := range sizes {
		f := newFixture(t, size)
		for part := int64(1); part <= f.layout.PartCount; part++ {
			off, n, err := f.layout.PlaintextRange(part)
			if err != nil {
				t.Fatal(err)
			}
			plain := f.plain[off : off+n]
			salt := [SaltBytes]byte{byte(size), byte(part), 7}
			whole := sealWith(t, f, part, salt, func(s *PartSealer) (io.Reader, error) {
				b, err := s.Seal(plain)
				return bytes.NewReader(b), err
			})
			fromSlice := sealWith(t, f, part, salt, func(s *PartSealer) (io.Reader, error) { return s.Reader(plain) })
			streamed := sealWith(t, f, part, salt, func(s *PartSealer) (io.Reader, error) {
				return s.StreamReader(iotest.HalfReader(bytes.NewReader(plain)))
			})
			if !bytes.Equal(whole, fromSlice) || !bytes.Equal(whole, streamed) {
				t.Fatalf("size %d part %d: Seal, Reader and StreamReader differ", size, part)
			}
			if n <= 3*SegmentPlaintextBytes+7 {
				oneByte := sealWith(t, f, part, salt, func(s *PartSealer) (io.Reader, error) {
					r, err := s.StreamReader(iotest.OneByteReader(bytes.NewReader(plain)))
					return iotest.OneByteReader(r), err
				})
				if !bytes.Equal(whole, oneByte) {
					t.Fatalf("size %d part %d: one-byte reads differ", size, part)
				}
			}
			got, err := OpenPart(f.key, f.id, f.layout, part, streamed)
			if err != nil || !bytes.Equal(got, plain) {
				t.Fatalf("size %d part %d: the streamed part does not open: %v", size, part, err)
			}
		}
	}
}

// sealWith seals a part under a fixed salt through seal and returns every byte it yields.
func sealWith(t *testing.T, f fixture, part int64, salt [SaltBytes]byte, seal func(*PartSealer) (io.Reader, error)) []byte {
	t.Helper()
	s, err := newPartSealerWithSalt(f.key, f.id, f.layout, part, salt)
	if err != nil {
		t.Fatal(err)
	}
	r, err := seal(s)
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(out)) != s.Len() {
		t.Fatalf("part %d: %d bytes, Len %d", part, len(out), s.Len())
	}
	return out
}

// TestVectorStreamedParts: StreamReader reproduces every part of the golden vectors.
func TestVectorStreamedParts(t *testing.T) {
	lv := loadVectors(t)
	for _, f := range lv.file.Files {
		c := lv.cases[f.Name]
		for i, p := range f.Parts {
			var salt [SaltBytes]byte
			copy(salt[:], unhex(t, p.Salt))
			s, err := newPartSealerWithSalt(c.dataKey, c.identity, c.layout, p.PartNumber, salt)
			if err != nil {
				t.Fatal(err)
			}
			off, n, err := c.layout.PlaintextRange(p.PartNumber)
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.StreamReader(bytes.NewReader(c.plain[off : off+n]))
			if err != nil {
				t.Fatal(err)
			}
			part, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(part)
			if int64(len(part)) != p.Length || hex.EncodeToString(sum[:]) != p.SHA256 || !bytes.Equal(part, c.parts[i]) {
				t.Fatalf("%s part %d: streamed %d bytes, sha256 %x; want %d, %s", f.Name, p.PartNumber, len(part), sum, p.Length, p.SHA256)
			}
			if p.Ciphertext != "" && hex.EncodeToString(part) != p.Ciphertext {
				t.Fatalf("%s part %d: streamed ciphertext differs from the vector", f.Name, p.PartNumber)
			}
		}
	}
}

var errVerdict = errors.New("the plaintext changed")

// verdictWithLastBytes returns errVerdict in the same Read as the last bytes of plain,
// then io.EOF on every later Read: a source whose verdict is not sticky.
type verdictWithLastBytes struct {
	plain []byte
	done  bool
}

func (v *verdictWithLastBytes) Read(p []byte) (int, error) {
	if v.done {
		return 0, io.EOF
	}
	n := copy(p, v.plain)
	v.plain = v.plain[n:]
	if len(v.plain) > 0 {
		return n, nil
	}
	v.done = true
	return n, errVerdict
}

// noProgress returns no bytes and no error on every Read.
type noProgress struct{}

func (noProgress) Read([]byte) (int, error) { return 0, nil }

type streamFailure struct {
	name   string
	size   int64
	source func(plain []byte) io.Reader
	want   error
	sealed int64 // segments the stream yields before it stops
}

// TestStreamReaderStopsBeforeTheFailingSegment: a source that ends early, runs past the
// part, fails, stalls, or returns a verdict in place of its final io.EOF or with its last
// bytes stops the stream before the failing segment is sealed, and the stream keeps
// returning that error.
func TestStreamReaderStopsBeforeTheFailingSegment(t *testing.T) {
	const size = 3*SegmentPlaintextBytes + 7
	cases := []streamFailure{
		{
			name:   "ends early",
			size:   size,
			source: func(plain []byte) io.Reader { return bytes.NewReader(plain[:len(plain)-1]) },
			want:   io.ErrUnexpectedEOF,
			sealed: 3,
		},
		{
			name:   "ends at a segment boundary",
			size:   size,
			source: func(plain []byte) io.Reader { return bytes.NewReader(plain[:2*SegmentPlaintextBytes]) },
			want:   io.ErrUnexpectedEOF,
			sealed: 2,
		},
		{
			name: "runs past the part",
			size: size,
			source: func(plain []byte) io.Reader {
				return io.MultiReader(bytes.NewReader(plain), bytes.NewReader([]byte{0}))
			},
			want:   ErrLayout,
			sealed: 3,
		},
		{
			name: "fails inside a segment",
			size: size,
			source: func(plain []byte) io.Reader {
				return io.MultiReader(bytes.NewReader(plain[:SegmentPlaintextBytes+5]), errReader{errVerdict})
			},
			want:   errVerdict,
			sealed: 1,
		},
		{
			name:   "returns a verdict in place of io.EOF",
			size:   size,
			source: func(plain []byte) io.Reader { return io.MultiReader(bytes.NewReader(plain), errReader{errVerdict}) },
			want:   errVerdict,
			sealed: 3,
		},
		{
			name:   "returns a verdict with its last bytes",
			size:   size,
			source: func(plain []byte) io.Reader { return &verdictWithLastBytes{plain: plain} },
			want:   errVerdict,
			sealed: 3,
		},
		{
			name:   "one segment with a verdict with its last bytes",
			size:   SegmentPlaintextBytes,
			source: func(plain []byte) io.Reader { return &verdictWithLastBytes{plain: plain} },
			want:   errVerdict,
			sealed: 0,
		},
		{
			name:   "returns nothing forever",
			size:   size,
			source: func([]byte) io.Reader { return noProgress{} },
			want:   io.ErrNoProgress,
			sealed: 0,
		},
		{
			name:   "empty part with a verdict",
			size:   0,
			source: func([]byte) io.Reader { return errReader{errVerdict} },
			want:   errVerdict,
			sealed: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.size)
			s, err := NewPartSealer(f.key, f.id, f.layout, 1)
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.StreamReader(tc.source(f.plain))
			if err != nil {
				t.Fatal(err)
			}
			out, err := io.ReadAll(r)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if want := SaltBytes + tc.sealed*SegmentBytes; int64(len(out)) != want {
				t.Fatalf("yielded %d bytes, want %d: the salt and %d segments", len(out), want, tc.sealed)
			}
			if n, again := r.Read(make([]byte, 1)); n != 0 || !errors.Is(again, tc.want) {
				t.Fatalf("a later read = %d, %v; want the same error", n, again)
			}
			o, err := NewPartOpener(f.key, f.id, f.layout, 1, out[:SaltBytes])
			if err != nil {
				t.Fatal(err)
			}
			for i := range tc.sealed {
				seg := out[SaltBytes+i*SegmentBytes : SaltBytes+(i+1)*SegmentBytes]
				if _, err := o.OpenSegment(nil, i, seg); err != nil {
					t.Fatalf("segment %d does not open: %v", i, err)
				}
			}
		})
	}
}

// TestStreamReaderSpendsTheSealer: StreamReader shares the sealer's single use with Seal
// and Reader, whatever the source then yields.
func TestStreamReaderSpendsTheSealer(t *testing.T) {
	f := newFixture(t, 10)
	s, err := NewPartSealer(f.key, f.id, f.layout, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StreamReader(errReader{errVerdict}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seal(f.plain); !errors.Is(err, ErrSealerUsed) {
		t.Fatalf("Seal after StreamReader: %v", err)
	}
	if _, err := s.StreamReader(bytes.NewReader(f.plain)); !errors.Is(err, ErrSealerUsed) {
		t.Fatalf("second StreamReader: %v", err)
	}
	s, err = NewPartSealer(f.key, f.id, f.layout, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seal(f.plain); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StreamReader(bytes.NewReader(f.plain)); !errors.Is(err, ErrSealerUsed) {
		t.Fatalf("StreamReader after Seal: %v", err)
	}
}

// zeros yields n zero bytes, then io.EOF, without allocating.
type zeros struct{ n int64 }

func (z *zeros) Read(p []byte) (int, error) {
	if z.n == 0 {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), z.n)]
	clear(p)
	z.n -= int64(len(p))
	return len(p), nil
}

// writerOnly hides io.Discard's ReadFrom, so io.CopyBuffer reads through the given
// buffer instead of a pooled one of its own.
type writerOnly struct{ io.Writer }

// streamAllocation streams part 1 of a file of size bytes and returns the bytes the
// sealing allocated on the heap.
func streamAllocation(t *testing.T, size int64) (allocated, plaintext int64) {
	t.Helper()
	l, err := PlanLayout(size)
	if err != nil {
		t.Fatal(err)
	}
	_, n, err := l.PlaintextRange(1)
	if err != nil {
		t.Fatal(err)
	}
	id := FileIdentity{Size: uint64(size)}
	key := make([]byte, dataKeyBytes)
	out := make([]byte, 32<<10)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	s, err := NewPartSealer(key, id, l, 1)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.StreamReader(&zeros{n: n})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyBuffer(writerOnly{io.Discard}, r, out); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc - before.TotalAlloc), n
}

// TestStreamReaderMemoryIsOneSegment: streaming a part allocates about one segment,
// whether the part holds one segment, the usual 64, or the 256 of a file of about
// 2.4 TiB.
func TestStreamReaderMemoryIsOneSegment(t *testing.T) {
	if testing.Short() {
		t.Skip("streams 256 MiB")
	}
	sizes := []int64{
		SegmentPlaintextBytes,
		MinSegmentsPerPart * SegmentPlaintextBytes,
		256 * MaxParts * SegmentPlaintextBytes,
	}
	var smallest int64
	for i, size := range sizes {
		allocated, plaintext := streamAllocation(t, size)
		t.Logf("a part of %d KiB allocated %d KiB", plaintext>>10, allocated>>10)
		if allocated > 2*SegmentBytes {
			t.Errorf("a part of %d bytes allocated %d bytes, want at most two segments", plaintext, allocated)
		}
		if i == 0 {
			smallest = allocated
		} else if allocated > smallest+64<<10 {
			t.Errorf("a part of %d bytes allocated %d bytes, %d more than a one-segment part", plaintext, allocated, allocated-smallest)
		}
	}
}

// BenchmarkStreamPart streams one full 64-segment part from a reader; compare its
// allocations with BenchmarkSealPart, which needs the part's whole plaintext.
func BenchmarkStreamPart(b *testing.B) {
	f := newFixture(b, MinSegmentsPerPart*SegmentPlaintextBytes)
	b.SetBytes(f.layout.Size)
	b.ReportAllocs()
	for b.Loop() {
		s, err := NewPartSealer(f.key, f.id, f.layout, 1)
		if err != nil {
			b.Fatal(err)
		}
		r, err := s.StreamReader(bytes.NewReader(f.plain))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, r); err != nil {
			b.Fatal(err)
		}
	}
}
