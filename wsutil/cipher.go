package wsutil

import (
	"io"

	"github.com/gobwas/ws"
)

type CipherReader struct {
	r    io.Reader
	mask []byte
	pos  int
}

func NewCipherReader(r io.Reader, mask []byte) *CipherReader {
	return &CipherReader{r, mask, 0}
}

func (c *CipherReader) Reset(r io.Reader, mask []byte) {
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
