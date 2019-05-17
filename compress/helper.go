package compress

import (
	"bytes"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/compress"
)

// CompressFrame returns frame with compressed payload and updated header.
// RSV1 bit and new length will be also set.
// NOTE: original frame will be changed.
func CompressFrame(f ws.Frame, level int) (ws.Frame, error) {
	// Only data frames should be compressed.
	if !f.Header.OpCode.IsData() {
		return f, nil
	}

	buf := bytes.NewBuffer(nil)
	compressor := compress.NewWriter(buf, level)
	defer compressor.Close()

	_, err := compressor.Write(f.Payload)
	if err != nil {
		return f, err
	}

	if f.Header.Fin {
		err = compressor.Flush()
	} else {
		err = compressor.FlushFragment()
	}
	if err != nil {
		return f, err
	}

	// Mark compressed and change length
	f.Header.Rsv |= 0x04
	f.Header.Length = int64(buf.Len())
	f.Payload = buf.Bytes()

	return f, nil
}

// Compress frame and panic if there is something wrong.
func MustCompressFrame(f ws.Frame, level int) ws.Frame {
	fr, err := CompressFrame(f, level)
	if err != nil {
		panic(err)
	}

	return fr
}
