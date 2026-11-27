package put

import (
	"io"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

// partSealer seals one plaintext of one part, streamed from a reader, as volumeformat.PartSealer does.
type partSealer interface {
	StreamReader(plaintext io.Reader) (io.Reader, error)
	Len() int64
}

// sealerFactory makes the sealer of one send of one part; every send takes a new one, so a fresh salt.
type sealerFactory func(dataKey []byte, id volumeformat.FileIdentity, l volumeformat.Layout, part int64) (partSealer, error)

func defaultSealer(dataKey []byte, id volumeformat.FileIdentity, l volumeformat.Layout, part int64) (partSealer, error) {
	return volumeformat.NewPartSealer(dataKey, id, l, part)
}

// newPartSealer makes a part's sealer through the factory the options name, or the format's own.
func (r *runner) newPartSealer(dataKey []byte, id volumeformat.FileIdentity, l volumeformat.Layout, part int64) (partSealer, error) {
	if r.o.newSealer != nil {
		return r.o.newSealer(dataKey, id, l, part)
	}
	return defaultSealer(dataKey, id, l, part)
}
