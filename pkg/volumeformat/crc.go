package volumeformat

import (
	"hash"
	"hash/crc64"
)

// crc64NVMETable is CRC-64/NVME: reflected polynomial 0x9a6c9329ac4bc9b5, init and
// xorout all ones, which hash/crc64 applies for any table.
var crc64NVMETable = crc64.MakeTable(0x9a6c9329ac4bc9b5)

// NewCRC64NVME returns the completed object's CRC-64/NVME hash. It
// covers the ciphertext as stored: each part's last acknowledged copy, salt included,
// since a resent part carries a fresh salt and so different bytes.
func NewCRC64NVME() hash.Hash64 { return crc64.New(crc64NVMETable) }
