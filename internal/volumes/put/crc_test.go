package put

import (
	"hash/crc64"
	"testing"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

// crcOf is the CRC-64/NVME of b, from the format package's hash.
func crcOf(b []byte) uint64 {
	h := volumeformat.NewCRC64NVME()
	_, _ = h.Write(b)
	return h.Sum64()
}

func TestCRC64NVMEPolynomial(t *testing.T) {
	if got := crcOf([]byte("123456789")); got != 0xae8b14860a799888 {
		t.Errorf("CRC-64/NVME of 123456789 = %016x, want ae8b14860a799888", got)
	}
	data := patterned(1000, 3)
	if got, want := crc64.Checksum(data, crc64.MakeTable(crc64NVMEPoly)), crcOf(data); got != want {
		t.Errorf("polynomial %x gives %016x, the format package %016x", uint64(crc64NVMEPoly), got, want)
	}
}

func TestCRC64Combine(t *testing.T) {
	for _, size := range []int{1, 2, 7, 16, 31, 32, 4096, 1<<20 + 13} {
		data := patterned(size, byte(size))
		for _, cut := range []int{0, 1, size / 3, size / 2, size - 1, size} {
			if cut < 0 || cut > size {
				continue
			}
			a, b := data[:cut], data[cut:]
			if got, want := crc64Combine(crcOf(a), crcOf(b), int64(len(b))), crcOf(data); got != want {
				t.Errorf("size %d cut at %d: combined %016x, want %016x", size, cut, got, want)
			}
		}
	}
}

func TestFileCRCFoldsParts(t *testing.T) {
	var object []byte
	var parts []partResult
	for i, size := range []int{32, 5000, 1 << 20, 77} {
		part := patterned(size, byte(i))
		object = append(object, part...)
		parts = append(parts, partResult{
			CRC: crcOf(part),
			Len: int64(len(part)),
		})
	}

	if got, want := fileCRC(parts), crcOf(object); got != want {
		t.Errorf("fileCRC = %016x, want %016x", got, want)
	}
	if got, want := fileCRC(parts[:1]), parts[0].CRC; got != want {
		t.Errorf("fileCRC of one part = %016x, want the part's %016x", got, want)
	}
}
