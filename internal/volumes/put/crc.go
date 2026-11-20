package put

// crc64NVMEPoly is the reflected CRC-64/NVME polynomial, as volumeformat.NewCRC64NVME uses.
const crc64NVMEPoly = 0x9a6c9329ac4bc9b5

const gf2Dim = 64

// gf2Matrix is a linear operator on 64-bit vectors over GF(2), column by column.
type gf2Matrix [gf2Dim]uint64

func (m *gf2Matrix) times(vec uint64) uint64 {
	var sum uint64
	for i := 0; vec != 0; i, vec = i+1, vec>>1 {
		if vec&1 != 0 {
			sum ^= m[i]
		}
	}
	return sum
}

func (m *gf2Matrix) square(src *gf2Matrix) {
	for n := range m {
		m[n] = src.times(src[n])
	}
}

// crc64Combine returns the CRC-64/NVME of A followed by B from their CRCs and the length of B.
func crc64Combine(crc1, crc2 uint64, len2 int64) uint64 {
	if len2 <= 0 {
		return crc1
	}
	var even, odd gf2Matrix
	odd[0] = crc64NVMEPoly
	row := uint64(1)
	for n := 1; n < gf2Dim; n++ {
		odd[n] = row
		row <<= 1
	}
	even.square(&odd)
	odd.square(&even)
	for {
		even.square(&odd)
		if len2&1 != 0 {
			crc1 = even.times(crc1)
		}
		len2 >>= 1
		if len2 == 0 {
			break
		}
		odd.square(&even)
		if len2&1 != 0 {
			crc1 = odd.times(crc1)
		}
		len2 >>= 1
		if len2 == 0 {
			break
		}
	}
	return crc1 ^ crc2
}

// fileCRC folds a file's part CRCs, in part order, into the CRC of the whole ciphertext.
func fileCRC(parts []partResult) uint64 {
	var crc uint64
	for i := range parts {
		crc = crc64Combine(crc, parts[i].CRC, parts[i].Len)
	}
	return crc
}
