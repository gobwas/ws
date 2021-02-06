package wsflate

import (
	"bytes"
	"testing"

	"github.com/gobwas/ws"
)

func TestHelperWriteAndRead(t *testing.T) {
	const text = "hello, wsflate!"
	f := ws.NewTextFrame([]byte(text))
	c, err := CompressFrame(f)
	if err != nil {
		t.Fatalf("can't compress frame: %v", err)
	}
	d, err := DecompressFrame(c)
	if err != nil {
		t.Fatalf("can't decompress frame: %v", err)
	}
	if f.Header != d.Header {
		t.Fatalf("original and decompressed headers are not equal")
	}
	if !bytes.Equal(f.Payload, d.Payload) {
		t.Fatalf("original and decompressed payload are not equal")
	}
}
