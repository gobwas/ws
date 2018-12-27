package compress

import (
	"bufio"
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"io/ioutil"

	"github.com/gobwas/pool/pbufio"
)

const (
	maxCompressionLevel = flate.BestCompression
	minCompressionLevel = -2
)

var (
	ErrWriteClose            = errors.New("write to closed writer")
	ErrUnexpectedEndOfStream = errors.New("websocket: internal error, unexpected bytes at end of flate stream")

	// Tail as described here: https://tools.ietf.org/html/rfc7692#section-7.2.2
	deflateFinal = [4]byte{0, 0, 0xff, 0xff}
	// Tail to prevent reader error
	tail = [5]byte{0x01, 0, 0, 0xff, 0xff}

	flateReaderPool   = NewFlateReaderPool()
	flateWritersPools = [maxCompressionLevel - minCompressionLevel + 1]*FlateWriterPool{
		NewFlateWriterPool(-2), NewFlateWriterPool(-1), NewFlateWriterPool(0),
		NewFlateWriterPool(1), NewFlateWriterPool(2), NewFlateWriterPool(3),
		NewFlateWriterPool(4), NewFlateWriterPool(5), NewFlateWriterPool(6),
		NewFlateWriterPool(7), NewFlateWriterPool(8), NewFlateWriterPool(9),
	}
)

type Reader interface {
	io.ReadCloser
	Reset(src io.Reader, dict []byte)
}

type reader struct {
	src         io.Reader
	buf         *bufio.Reader
	flateReader io.ReadCloser
}

func NewReader(src io.Reader, bufferSize int) Reader {
	// Create buffered reader to buffer size control.
	// Otherwise it will be default buffer size from flate package. Flate
	// package also smart enough to do not wrap already buffered reader again.
	buf := pbufio.GetReader(
		io.MultiReader(
			src,
			bytes.NewReader(deflateFinal[:]),
			bytes.NewReader(tail[:]),
		),
		bufferSize+len(deflateFinal)+len(tail),
	)

	return &reader{
		src:         src,
		buf:         buf,
		flateReader: flateReaderPool.Get(buf),
	}
}

func (cr *reader) Reset(src io.Reader, dict []byte) {
	cr.src = src
	cr.reset(dict)
}

func (cr *reader) reset(dict []byte) {
	cr.buf.Reset(io.MultiReader(
		cr.src,
		bytes.NewReader(deflateFinal[:]),
		bytes.NewReader(tail[:]),
	))
	cr.flateReader.(flate.Resetter).Reset(cr.buf, dict)
}

func (cr *reader) Read(p []byte) (n int, err error) {
	if cr.flateReader == nil || cr.src == nil || cr.buf == nil {
		return 0, io.ErrClosedPipe
	}

	n, err = cr.flateReader.Read(p)
	// When multiple DEFLATE block in one message was used — there is can be
	// io.EOF that actually means only end of the flate block, not the message.
	// To workaround that case we check internal buffer for content and if there
	// is anything in it — just ignore io.EOF to prevent partial message read.
	// Multiple DEFLATE block is supported in permessage-deflate.
	// See: https://tools.ietf.org/html/rfc7692#section-7.2.3.5
	if err == io.EOF {
		_, err2 := cr.buf.ReadByte()
		if err2 == io.EOF {
			cr.reset(nil)
			return n, io.EOF
		}
		cr.buf.UnreadByte()
	}

	if err != nil {
		cr.reset(nil)
	}

	return
}

// Close Reader and return resources.
func (cr *reader) Close() error {
	if cr.flateReader == nil || cr.src == nil || cr.buf == nil {
		return io.ErrClosedPipe
	}

	cr.src = nil

	flateReaderPool.Put(cr.flateReader)
	cr.flateReader = nil

	pbufio.PutReader(cr.buf)
	cr.buf = nil

	return nil
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

func (tw *truncWriter) FlushTail() error {
	_, err := tw.origin.Write(tw.endBuffer[:])
	tw.endBuffer = [4]byte{0, 0, 0, 0}
	tw.filledEndBytes = 0

	return err
}

// Compress writer
// Only client_no_context_takeover supported now. For implement sliding window
// there is no API in flate package.
//
// See: https://tools.ietf.org/html/rfc7692#section-7.1.1
// See: https://github.com/golang/go/issues/3155
type Writer interface {
	io.WriteCloser

	Flush() error
	FlushFragment() error
	Reset(io.Writer)
}

type writer struct {
	flateWriter *flate.Writer
	truncWriter *truncWriter
	dst         io.Writer

	level        int
	writeStarted bool
}

func NewWriter(w io.Writer, level int) Writer {
	tw := &truncWriter{origin: w}
	return &writer{
		truncWriter: tw,
		flateWriter: flateWritersPools[level-minCompressionLevel].Get(tw),
		dst:         w,
		level:       level,
	}
}

func (cw *writer) ReadFrom(src io.Reader) (n int64, err error) {
	bts, err := ioutil.ReadAll(src)
	if err != nil {
		return 0, err
	}

	m, err := cw.Write(bts)
	return int64(m), err
}

func (cw *writer) Write(p []byte) (n int, err error) {
	// Here is dirty hack to handle empty messages properly.
	defer func() {
		cw.writeStarted = err == nil && len(p) > 0
	}()

	if cw.flateWriter == nil {
		return 0, ErrWriteClose
	}

	return cw.flateWriter.Write(p)
}

func (cw *writer) FlushFragment() error {
	err := cw.flateWriter.Flush()
	if err != nil {
		return err
	}

	return cw.truncWriter.FlushTail()
}

// Flush
func (cw *writer) Flush() error {
	defer func() {
		// Do not share state between flushes.
		cw.Reset(cw.dst)

		cw.writeStarted = false
	}()

	// The writeStarted flag needed because flateWriter have different
	// representation for an empty message. It write at least Z_SYNC_FLUSH marker
	// but that not expected.
	// TODO: May be better solution should include buffer for small messages
	//       that should be excluded from compression and sends as is.
	if !cw.writeStarted {
		return nil
	}

	err := cw.flateWriter.Flush()
	if err != nil {
		return err
	}

	return nil
}

func (cw *writer) Close() error {
	if cw.flateWriter == nil {
		return ErrWriteClose
	}

	err1 := cw.Flush()
	flateWritersPools[cw.level-minCompressionLevel].Put(cw.flateWriter)
	cw.flateWriter = nil
	cw.writeStarted = false

	if cw.truncWriter.endBuffer != deflateFinal &&
		cw.truncWriter.endBuffer != [4]byte{0, 0, 0, 0} {
		return ErrUnexpectedEndOfStream
	}
	cw.truncWriter.Reset(nil)

	if err1 != nil {
		return err1
	}

	return err1
}

func (cw *writer) Reset(w io.Writer) {
	cw.writeStarted = false

	cw.dst = w
	cw.truncWriter.Reset(cw.dst)
	cw.flateWriter.Reset(cw.truncWriter)
}
