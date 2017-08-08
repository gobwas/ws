package wsutil

import (
	"io"

	"github.com/gobwas/pool/pbytes"
	"github.com/gobwas/ws"
)

type CipherReader struct {
	r    io.Reader
	mask [4]byte
	pos  int
}

func NewCipherReader(r io.Reader, mask [4]byte) *CipherReader {
	return &CipherReader{r, mask, 0}
}

func (c *CipherReader) Reset(r io.Reader, mask [4]byte) {
	c.r = r
	c.mask = mask
	c.pos = 0
}

func (c *CipherReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	ws.Cipher(p[:n], c.mask, c.pos)
	c.pos += n
	return
}

type CipherWriter struct {
	w    io.Writer
	mask [4]byte
	pos  int
}

func NewCipherWriter(w io.Writer, mask [4]byte) *CipherWriter {
	return &CipherWriter{w, mask, 0}
}

func (c *CipherWriter) Reset(w io.Writer, mask [4]byte) {
	c.w = w
	c.mask = mask
	c.pos = 0
}

func (c *CipherWriter) Write(p []byte) (n int, err error) {
	cp := pbytes.GetLen(len(p))
	defer pbytes.Put(cp)

	copy(cp, p)
	ws.Cipher(cp, c.mask, c.pos)
	n, err = c.w.Write(cp)
	c.pos += n

	return
}
