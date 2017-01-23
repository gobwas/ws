package wsutil

import (
	"io"

	"github.com/gobwas/pool"
	"github.com/gobwas/pool/pbytes"
	"github.com/gobwas/ws"
)

const defaultWriteBuffer = 4096

type Writer struct {
	wr  io.Writer
	buf []byte
	n   int

	dirty  bool
	frames int

	op   ws.OpCode
	mask bool
}

var writers = pool.MakePoolMap(128, 65536)

// GetWriter tries to reuse writer and get it from the pool.
// If no writer is found, it calls NewWriterSize.
//
// Using this function is useful for memory consumption optimizations,
// because NewWriter and NewWriterSize makes allocations for inner bytes buffer.
//
// If you have your own bytes buffer pool (pool of []byte) you could
// use NewWriterBuffer to use pooled bytes in writer.
func GetWriter(dst io.Writer, op ws.OpCode, mask bool, n int) *Writer {
	n = pool.CeilToPowerOfTwo(n)
	if p, ok := writers[n]; ok {
		if w := p.Get(); w != nil {
			ret := w.(*Writer)
			ret.Reset(dst, op, mask)
			return ret
		}
	}
	return NewWriterSize(dst, op, mask, n)
}

// PutWriter puts w for future reuse by GetWriter.
func PutWriter(w *Writer) {
	n := pool.CeilToPowerOfTwo(len(w.buf))
	if p, ok := writers[n]; ok {
		w.Reset(nil, 0, false)
		p.Put(w)
	}
}

func NewWriter(dst io.Writer, op ws.OpCode, mask bool) *Writer {
	return NewWriterSize(dst, op, mask, 0)
}

func NewWriterSize(dst io.Writer, op ws.OpCode, mask bool, n int) *Writer {
	if n <= 0 {
		n = defaultWriteBuffer
	}
	return NewWriterBuffer(dst, op, mask, make([]byte, n))
}

func NewWriterBuffer(wr io.Writer, op ws.OpCode, mask bool, buf []byte) *Writer {
	return &Writer{
		wr:   wr,
		buf:  buf,
		op:   op,
		mask: mask,
	}
}

func (w *Writer) Write(p []byte) (n int, err error) {
	// Even if len(p) == 0 we mark w as dirty,
	// cause even empty p (and empty frame) may have a value.
	w.dirty = true

	if len(p) > len(w.buf) && w.n == 0 {
		// Large write.
		return w.write(p)
	}
	for {
		nn := copy(w.buf[w.n:], p)
		p = p[nn:]
		w.n += nn
		n += nn

		if len(p) == 0 {
			break
		}

		_, err = w.write(w.buf)
		if err != nil {
			break
		}
		w.n = 0
	}
	return
}

func (w *Writer) ReadFrom(src io.Reader) (n int64, err error) {
	var nn int
	for {
		if w.n == len(w.buf) { // buffer is full.
			if _, err = w.write(w.buf); err != nil {
				return
			}
			w.n = 0
		}

		nn, err = src.Read(w.buf[w.n:])
		w.n += nn
		n += int64(nn)
		w.dirty = true

		if err != nil {
			break
		}
	}
	if err == io.EOF {
		err = nil
	}
	return
}

func (w *Writer) Reset(wr io.Writer, op ws.OpCode, mask bool) {
	w.n = 0
	w.wr = wr
	w.op = op
	w.mask = mask
}

func (w *Writer) Flush() error {
	_, err := w.flush()
	return err
}

func (w *Writer) opCode() ws.OpCode {
	if w.frames > 0 {
		return ws.OpContinuation
	} else {
		return w.op
	}
}

func (w *Writer) flush() (n int, err error) {
	if w.n == 0 && !w.dirty {
		return 0, nil
	}

	n, err = w.writeFrame(w.opCode(), w.buf[:w.n], true)
	w.dirty = false
	w.n = 0
	w.frames = 0

	return
}

func (w *Writer) write(p []byte) (n int, err error) {
	return w.writeFrame(w.opCode(), p, false)
}

func (w *Writer) writeFrame(op ws.OpCode, p []byte, fin bool) (n int, err error) {
	header := ws.Header{
		OpCode: op,
		Length: int64(len(p)),
		Fin:    fin,
	}

	payload := p
	if w.mask {
		header.Mask = ws.NewMask()

		payload = pbytes.GetBufLen(len(p))
		defer pbytes.PutBuf(payload)

		copy(payload, p)
		ws.Cipher(payload, header.Mask, 0)
	}

	err = ws.WriteHeader(w.wr, header)
	if err == nil {
		n, err = w.wr.Write(payload)
	}

	w.frames++

	return
}
