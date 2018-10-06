package wsutil

import (
	"bytes"
	"compress/flate"
	"errors"
	"github.com/gobwas/ws"
	"io"
)

var (
	ErrWriteClose            = errors.New("write to closed writer")
	ErrUnexpectedEndOfStream = errors.New("websocket: internal error, unexpected bytes at end of flate stream")
	ErrStreamNotEmpty = errors.New("not empty stream")

	// Tail as described here: https://tools.ietf.org/html/rfc7692#section-7.2.2
	deflateFinal = [4]byte{0, 0, 0xff, 0xff}
	// Tail to prevent reader error
	tail = [5]byte{0x01, 0, 0, 0xff, 0xff}
)

type CompressReader interface {
	io.ReadCloser

	Reset(*Reader, []byte)
	NextFrame() (hdr ws.Header, err error)
}

type compressReader struct {
	reader      *Reader
	flateReader io.ReadCloser
	compressed  bool
	started     bool
}

func NewCompressReader(r *Reader) CompressReader {
	return &compressReader{
		reader: r,
		flateReader: flate.NewReader(
			io.MultiReader(
				r,
				bytes.NewReader(deflateFinal[:]),
				bytes.NewReader(tail[:]),
			),
		),
	}
}

func (cr *compressReader) Reset(r *Reader, dict []byte) {
	cr.reader = r
	cr.flateReader.(flate.Resetter).Reset(
		io.MultiReader(
			r,
			bytes.NewReader(deflateFinal[:]),
			bytes.NewReader(tail[:]),
		),
		dict,
	)
}

func (cr *compressReader) NextFrame() (hdr ws.Header, err error) {
	hdr, err = cr.reader.NextFrame()
	if err != nil {
		return
	}

	cr.started = true
	cr.compressed = hdr.Rsv1()

	return
}

func (cr *compressReader) Read(p []byte) (int, error) {
	if cr.flateReader == nil {
		return 0, io.ErrClosedPipe
	}

	if cr.compressed {
		n, err := cr.flateReader.Read(p)
		if err != nil {
			cr.started = false
			cr.compressed = false
		}

		return n, err
	}

	// If no RSV1 bit set think that there is not compressed message and read it
	// as usual.
	n, err := cr.reader.Read(p)
	// When read ends at least io.EOF will be here to mark message as ended.
	if err != nil {
		cr.started = false
		cr.compressed = false
	}

	return n, err
}

// Close Reader and return resources.
func (cr *compressReader) Close() error {
	if cr.flateReader == nil {
		return io.ErrClosedPipe
	}

	cr.reader = nil
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

	Flush() error
	FlushFragment() error
	Reset(*Writer)
}

type compressWriter struct {
	flateWriter *flate.Writer
	truncWriter *truncWriter
	dst         *Writer
}

func NewCompressWriter(w *Writer, level int) (CompressWriter, error) {
	if w.dirty || w.Buffered() != 0 {
		return nil, ErrNotEmpty
	}

	w.compressed = true

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

	return
}

func (cw *compressWriter) Flush() error {
	err := cw.flateWriter.Flush()
	if err != nil {
		return err
	}

	// Do not share state between flushes.
	cw.Reset(cw.dst)

	return cw.dst.Flush()
}

func (cw *compressWriter) FlushFragment() error {
	err := cw.flateWriter.Flush()
	if err != nil {
		return err
	}

	// Do not share state between flushes.
	cw.Reset(cw.dst)

	return cw.dst.FlushFragment()
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

func (cw *compressWriter) Reset(w *Writer) {
	cw.dst = w
	cw.truncWriter.Reset(w)
	cw.flateWriter.Reset(cw.truncWriter)
}
