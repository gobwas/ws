package wsflate

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"

	"github.com/gobwas/ws"
)

// DefaultHelper is a default helper instance holding standard library's
// `compress/flate` compressor and decompressor under the hood.
//
// Note that use of DefaultHelper methods assumes that DefaultParameters were
// used for extension negotiation during WebSocket handshake.
var DefaultHelper = Helper{
	Compressor: func(w io.Writer) Compressor {
		// No error can be returned here as NewWriter() doc says.
		f, _ := flate.NewWriter(w, 9)
		return f
	},
	Decompressor: func(r io.Reader) Decompressor {
		return flate.NewReader(r)
	},
}

// DefaultParameters holds deflate extension parameters which are assumed by
// DefaultHelper to be used during WebSocket handshake.
var DefaultParameters = Parameters{
	ServerNoContextTakeover: true,
	ClientNoContextTakeover: true,
}

// CompressFrame is a shortcut for DefaultHelper.CompressFrame().
//
// Note that use of DefaultHelper methods assumes that DefaultParameters were
// used for extension negotiation during WebSocket handshake.
func CompressFrame(f ws.Frame) (ws.Frame, error) {
	return DefaultHelper.CompressFrame(f)
}

// CompressFrameBuffer is a shortcut for DefaultHelper.CompressFrameBuffer().
//
// Note that use of DefaultHelper methods assumes that DefaultParameters were
// used for extension negotiation during WebSocket handshake.
func CompressFrameBuffer(buf Buffer, f ws.Frame) (ws.Frame, error) {
	return DefaultHelper.CompressFrameBuffer(buf, f)
}

// DecompressFrame is a shortcut for DefaultHelper.DecompressFrame().
//
// Note that use of DefaultHelper methods assumes that DefaultParameters were
// used for extension negotiation during WebSocket handshake.
func DecompressFrame(f ws.Frame) (ws.Frame, error) {
	return DefaultHelper.DecompressFrame(f)
}

// DecompressFrameBuffer is a shortcut for
// DefaultHelper.DecompressFrameBuffer().
//
// Note that use of DefaultHelper methods assumes that DefaultParameters were
// used for extension negotiation during WebSocket handshake.
func DecompressFrameBuffer(buf Buffer, f ws.Frame) (ws.Frame, error) {
	return DefaultHelper.DecompressFrameBuffer(buf, f)
}

// Helper is a helper struct that holds common code for compression and
// decompression bytes or WebSocket frames.
//
// Its purpose is to reduce boilerplate code in WebSocket applications.
type Helper struct {
	Compressor   func(w io.Writer) Compressor
	Decompressor func(r io.Reader) Decompressor
}

// Buffer is an interface representing some bytes buffering object.
type Buffer interface {
	io.Writer
	Bytes() []byte
}

// CompressFrame returns compressed version of a frame.
// Note that it does memory allocations internally. To control those
// allocations consider using CompressFrameBuffer().
func (h *Helper) CompressFrame(in ws.Frame) (f ws.Frame, err error) {
	var buf bytes.Buffer
	return h.CompressFrameBuffer(&buf, in)
}

// DecompressFrame returns decompressed version of a frame.
// Note that it does memory allocations internally. To control those
// allocations consider using DecompressFrameBuffer().
func (h *Helper) DecompressFrame(in ws.Frame) (f ws.Frame, err error) {
	var buf bytes.Buffer
	return h.DecompressFrameBuffer(&buf, in)
}

// CompressFrameBuffer compresses a frame using given buffer.
// Returned frame's payload holds bytes returned by buf.Bytes().
func (h *Helper) CompressFrameBuffer(buf Buffer, f ws.Frame) (ws.Frame, error) {
	if !f.Header.Fin {
		return f, fmt.Errorf("wsflate: fragmented messages are not allowed")
	}
	if err := h.CompressTo(buf, f.Payload); err != nil {
		return f, err
	}
	var err error
	f.Payload = buf.Bytes()
	f.Header.Length = int64(len(f.Payload))
	f.Header, err = SetBit(f.Header)
	if err != nil {
		return f, err
	}
	return f, nil
}

// DecompressFrameBuffer decompresses a frame using given buffer.
// Returned frame's payload holds bytes returned by buf.Bytes().
func (h *Helper) DecompressFrameBuffer(buf Buffer, f ws.Frame) (ws.Frame, error) {
	if !f.Header.Fin {
		return f, fmt.Errorf(
			"wsflate: fragmented messages are not supported by helper",
		)
	}
	var (
		compressed bool
		err        error
	)
	f.Header, compressed, err = UnsetBit(f.Header)
	if err != nil {
		return f, err
	}
	if !compressed {
		return f, nil
	}
	if err := h.DecompressTo(buf, f.Payload); err != nil {
		return f, err
	}

	f.Payload = buf.Bytes()
	f.Header.Length = int64(len(f.Payload))

	return f, nil
}

// Compress compresses given bytes.
// Note that it does memory allocations internally. To control those
// allocations consider using CompressTo().
func (h *Helper) Compress(p []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := h.CompressTo(&buf, p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decompress decompresses given bytes.
// Note that it does memory allocations internally. To control those
// allocations consider using DecompressTo().
func (h *Helper) Decompress(p []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := h.DecompressTo(&buf, p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CompressTo compresses bytes into given buffer.
func (h *Helper) CompressTo(w io.Writer, p []byte) (err error) {
	c := NewWriter(w, h.Compressor)
	if _, err = c.Write(p); err != nil {
		return err
	}
	if err := c.Flush(); err != nil {
		return err
	}
	if err := c.Close(); err != nil {
		return err
	}
	return nil
}

// DecompressTo decompresses bytes into given buffer.
// Returned bytes are bytes returned by buf.Bytes().
func (h *Helper) DecompressTo(w io.Writer, p []byte) (err error) {
	fr := NewReader(bytes.NewReader(p), h.Decompressor)
	if _, err = io.Copy(w, fr); err != nil {
		return err
	}
	if err := fr.Close(); err != nil {
		return err
	}
	return nil
}
