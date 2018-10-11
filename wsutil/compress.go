package wsutil

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"io/ioutil"
	"sync"
)

const (
	maxCompressionLevel = flate.BestCompression
	minCompressionLevel = -2
)

var (
	ErrWriteClose            = errors.New("write to closed writer")
	ErrUnexpectedEndOfStream = errors.New("websocket: internal error, unexpected bytes at end of flate stream")
	ErrStreamNotEmpty        = errors.New("not empty stream")

	// Tail as described here: https://tools.ietf.org/html/rfc7692#section-7.2.2
	deflateFinal = [4]byte{0, 0, 0xff, 0xff}
	// Tail to prevent reader error
	tail = [5]byte{0x01, 0, 0, 0xff, 0xff}

	flateWriterPools [maxCompressionLevel - minCompressionLevel + 1]sync.Pool
	flateReaderPool  = sync.Pool{New: func() interface{} {
		return flate.NewReader(nil)
	}}
	flateReaderBuffers = sync.Pool{New: func() interface{} {
		return &bytes.Buffer{}
	}}
)

type CompressReader interface {
	io.ReadCloser
	io.ReaderFrom
}

type compressReader struct {
	buf         *bytes.Buffer
	flateReader io.ReadCloser
}

func NewCompressReader(
	bts []byte,
) (CompressReader, error) {
	var buf *bytes.Buffer
	if bts == nil {
		buf = flateReaderBuffers.Get().(*bytes.Buffer)
	} else {
		buf = bytes.NewBuffer(bts)
	}

	fr := flateReaderPool.Get().(io.ReadCloser)
	if err := fr.(flate.Resetter).Reset(
		io.MultiReader(
			buf,
			bytes.NewReader(deflateFinal[:]),
			bytes.NewReader(tail[:]),
		),
		nil,
	); err != nil {
		return nil, err
	}

	return &compressReader{buf: buf, flateReader: fr}, nil
}

func (cr *compressReader) reset(dict []byte) {
	cr.buf.Reset()
	cr.flateReader.(flate.Resetter).Reset(
		io.MultiReader(
			cr.buf,
			bytes.NewReader(deflateFinal[:]),
			bytes.NewReader(tail[:]),
		),
		dict,
	)
}

func (cr *compressReader) ReadFrom(r io.Reader) (n int64, err error) {
	return cr.buf.ReadFrom(r)
}

func (cr *compressReader) Read(p []byte) (n int, err error) {
	if cr.flateReader == nil {
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
func (cr *compressReader) Close() error {
	if cr.flateReader == nil {
		return io.ErrClosedPipe
	}

	err := cr.flateReader.Close()
	flateReaderPool.Put(cr.flateReader)
	cr.flateReader = nil
	flateReaderBuffers.Put(cr.buf)
	cr.buf = nil

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

func (tw *truncWriter) FlushTail() error {
	_, err := tw.origin.Write(tw.endBuffer[:])
	tw.endBuffer = [4]byte{0, 0, 0, 0}

	return err
}

// Compress writer
// Only client_no_context_takeover supported now. For implement sliding window
// there is no API in flate package.
//
// See: https://tools.ietf.org/html/rfc7692#section-7.1.1
// See: https://github.com/golang/go/issues/3155
type CompressWriter interface {
	io.WriteCloser

	Flush() error
	FlushFragment() error
	Reset(io.Writer)
}

type compressWriter struct {
	flateWriter *flate.Writer
	truncWriter *truncWriter
	dst         io.Writer

	level        int
	writeStarted bool
}

func NewCompressWriter(w io.Writer, level int) (CompressWriter, error) {
	tw := &truncWriter{origin: w}

	pool := &flateWriterPools[level - minCompressionLevel]
	fw, _ := pool.Get().(*flate.Writer)
	if fw == nil {
		var err error
		fw, err = flate.NewWriter(tw, level)
		if err != nil {
			return nil, err
		}
	} else {
		fw.Reset(tw)
	}

	return &compressWriter{
		truncWriter: tw,
		flateWriter: fw,
		dst:         w,
		level:       level,
	}, nil
}

func (cw *compressWriter) ReadFrom(src io.Reader) (n int64, err error) {
	bts, err := ioutil.ReadAll(src)
	if err != nil {
		return 0, err
	}

	m, err := cw.Write(bts)
	return int64(m), err
}

func (cw *compressWriter) Write(p []byte) (n int, err error) {
	// Here is dirty hack to handle empty messages properly.
	defer func() {
		cw.writeStarted = err == nil && len(p) > 0
	}()

	if cw.flateWriter == nil {
		return 0, ErrWriteClose
	}

	return cw.flateWriter.Write(p)
}

func (cw *compressWriter) FlushFragment() error {
	err := cw.flateWriter.Flush()
	if err != nil {
		return err
	}

	return cw.truncWriter.FlushTail()
}

// Flush
func (cw *compressWriter) Flush() error {
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

func (cw *compressWriter) Close() error {
	if cw.flateWriter == nil {
		return ErrWriteClose
	}

	err1 := cw.Flush()
	flateWriterPools[cw.level - minCompressionLevel].Put(cw.flateWriter)
	cw.flateWriter = nil
	cw.writeStarted = false

	if cw.truncWriter.endBuffer != deflateFinal ||
		cw.truncWriter.endBuffer != [4]byte{0, 0, 0, 0} {
		return ErrUnexpectedEndOfStream
	}
	cw.truncWriter.Reset(nil)

	if err1 != nil {
		return err1
	}

	return err1
}

func (cw *compressWriter) Reset(w io.Writer) {
	cw.writeStarted = false
	cw.dst = w
	cw.truncWriter.Reset(w)
	cw.flateWriter.Reset(cw.truncWriter)
}
