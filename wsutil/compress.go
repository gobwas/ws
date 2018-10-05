package wsutil

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
)

var (
	ErrWriteClose            = errors.New("write to closed writer")
	ErrUnexpectedEndOfStream = errors.New("websocket: internal error, unexpected bytes at end of flate stream")

	// Tail as described here: https://tools.ietf.org/html/rfc7692#section-7.2.2
	deflateFinal = [4]byte{
		// Bytes from RFC
		0, 0, 0xff, 0xff,
	}
	// Tail to prevent reader error
	tail = []byte{0x01, 0, 0, 0xff, 0xff}
)

type CompressorReader interface {
	io.ReadCloser

	Reset(io.Reader, []byte)
}

type compressReader struct {
	flateReader io.ReadCloser
}

func NewCompressReader(r io.Reader) CompressorReader {
	return &compressReader{
		flateReader: flate.NewReader(
			io.MultiReader(
				r,
				bytes.NewReader(deflateFinal[:]),
				bytes.NewReader(tail[:]),
			),
		),
	}
}

func (cr *compressReader) Reset(r io.Reader, dict []byte) {
	cr.flateReader.(flate.Resetter).Reset(
		io.MultiReader(
			r,
			bytes.NewReader(deflateFinal[:]),
			bytes.NewReader(tail[:]),
		),
		dict,
	)
}

func (cr *compressReader) Read(p []byte) (int, error) {
	if cr.flateReader == nil {
		return 0, io.ErrClosedPipe
	}

	n, err := cr.flateReader.Read(p)
	if err == io.EOF {
		cr.Close()
	}

	return n, err
}

// Close Reader and return resources.
func (cr *compressReader) Close() error {
	if cr.flateReader == nil {
		return io.ErrClosedPipe
	}

	err := cr.flateReader.Close()
	cr.flateReader = nil

	return err
}

// truncWriter is an io.Writer that writes all but the last four bytes of the
// stream to another io.Writer.
// Things related to: https://tools.ietf.org/html/rfc7692#section-7.2.1
type truncWriter struct {
	origin         io.Writer
	filledEndBytes int
	endBuffer      [4]byte
}

func (tw *truncWriter) Reset(w io.Writer) {
	tw.endBuffer = [4]byte{0, 0, 0, 0}
	tw.filledEndBytes = 0
	tw.origin = w
}

func (tw *truncWriter) Write(block []byte) (int, error) {
	filledBytes := 0

	// there we try to write «pruned» bytes from the end to special buffer.
	// that buffer appends to next block and will be replaced by end of that
	// block.
	if tw.filledEndBytes < len(tw.endBuffer) {
		filledBytes = copy(tw.endBuffer[tw.filledEndBytes:], block)
		block = block[filledBytes:]
		tw.filledEndBytes += filledBytes
		if len(block) == 0 {
			return filledBytes, nil
		}
	}

	m := len(block)
	if m > len(tw.endBuffer) {
		m = len(tw.endBuffer)
	}

	// Write buffer to the wire — we have replacement for it in that block.
	// If error — stops and return numbers of bytes writen.
	if nn, err := tw.origin.Write(tw.endBuffer[:m]); err != nil {
		return filledBytes + nn, err
	}

	// renew buffer and trim new buffer from the block end
	copy(tw.endBuffer[:], tw.endBuffer[m:])
	copy(tw.endBuffer[len(tw.endBuffer)-m:], block[len(block)-m:])

	// write block without last bytes.
	nn, err := tw.origin.Write(block[:len(block)-m])
	return filledBytes + nn, err
}

type CompressWriter interface {
	io.WriteCloser

	Reset(io.Writer)
}

type compressWriter struct {
	flateWriter *flate.Writer
	truncWriter *truncWriter
	dst         io.Writer
}

func NewCompressWriter(w io.Writer, level int) (io.Writer, error) {
	tw := &truncWriter{origin: w}
	flateWriter, err := flate.NewWriter(tw, level)
	if err != nil {
		return nil, err
	}

	return &compressWriter{
		truncWriter: tw,
		flateWriter: flateWriter,
		dst:         w,
	}, nil
}

func (cw *compressWriter) Write(p []byte) (n int, err error) {
	if cw.flateWriter == nil {
		return 0, ErrWriteClose
	}

	n, err = cw.flateWriter.Write(p)
	if err != nil {
		return
	}

	err = cw.flateWriter.Flush()

	return
}

func (cw *compressWriter) Close() error {
	if cw.flateWriter == nil {
		return ErrWriteClose
	}

	err1 := cw.flateWriter.Flush()
	cw.flateWriter = nil

	if cw.truncWriter.endBuffer != deflateFinal ||
		cw.truncWriter.endBuffer != [4]byte{0, 0, 0, 0} {
		return ErrUnexpectedEndOfStream
	}
	cw.truncWriter.Reset(nil)

	return err1
}

func (cw *compressWriter) Reset(w io.Writer) {
	cw.dst = w
	cw.truncWriter.Reset(cw)
	cw.flateWriter.Reset(cw.truncWriter)
}