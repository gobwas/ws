package wsflate

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"testing"
)

type errorDecompressor struct {
	reads    int
	closes   int
	readErr  error
	closeErr error
}

func (d *errorDecompressor) Read(p []byte) (int, error) {
	d.reads++
	if d.reads == 1 {
		return copy(p, "x"), d.readErr
	}
	return copy(p, "y"), nil
}

func (d *errorDecompressor) Close() error {
	d.closes++
	return d.closeErr
}

func TestReaderReadErrorIsSticky(t *testing.T) {
	readErr := errors.New("decompression failed")
	closeErr := errors.New("close failed")
	d := &errorDecompressor{readErr: readErr, closeErr: closeErr}
	r := NewReader(bytes.NewReader(nil), func(io.Reader) Decompressor { return d })
	p := make([]byte, 1)
	if n, err := r.Read(p); n != 1 || err != readErr || p[0] != 'x' {
		t.Errorf(
			"first Read() = %d, %v, %q; want 1, read error, x",
			n, err, p,
		)
	}
	if err := r.Err(); err != readErr {
		t.Errorf(
			"Err() = %v; want read error",
			err,
		)
	}
	if n, err := r.Read(p); n != 0 || err != readErr || d.reads != 1 {
		t.Errorf(
			"second Read() = %d, %v, calls=%d; want 0, read error, 1",
			n, err, d.reads,
		)
	}
	if err := r.Close(); err != readErr || d.closes != 0 {
		t.Errorf(
			"Close() = %v, calls=%d; want original read error, 0",
			err, d.closes,
		)
	}
	// Reset must clear the saved error and construct a fresh decompressor.
	fresh := &errorDecompressor{}
	r.ctor = func(io.Reader) Decompressor { return fresh }
	r.Reset(bytes.NewReader(nil))
	if err := r.Err(); err != nil {
		t.Errorf(
			"Err() after Reset() = %v; want nil",
			err,
		)
	}
	if n, err := r.Read(p); n != 1 || err != nil || fresh.reads != 1 {
		t.Errorf(
			"Read() after Reset() = %d, %v, calls=%d; want 1, nil, 1",
			n, err, fresh.reads,
		)
	}
}

func TestReaderEOFStillClosesDecompressor(t *testing.T) {
	d := &errorDecompressor{readErr: io.EOF}
	r := NewReader(bytes.NewReader(nil), func(io.Reader) Decompressor { return d })
	p := make([]byte, 1)
	if n, err := r.Read(p); n != 1 || err != io.EOF {
		t.Fatalf(
			"Read() = %d, %v; want 1, EOF",
			n, err,
		)
	}
	if err := r.Err(); err != nil {
		t.Fatalf(
			"Err() after EOF = %v; want nil",
			err,
		)
	}
	if err := r.Close(); err != nil || d.closes != 1 {
		t.Fatalf(
			"Close() = %v, calls=%d; want nil, 1",
			err, d.closes,
		)
	}
}

func TestReaderCorruptionSetsErr(t *testing.T) {
	// BTYPE=3 is a reserved DEFLATE block type.
	r := NewReader(bytes.NewReader([]byte{0x07}), func(src io.Reader) Decompressor {
		return flate.NewReader(src)
	})
	_, err := io.ReadAll(r)
	if err == nil || err == io.EOF {
		t.Fatalf(
			"ReadAll() error = %v; want corruption error",
			err,
		)
	}
	if got := r.Err(); got != err {
		t.Fatalf(
			"Err() = %v; want %v",
			got, err,
		)
	}
}

func BenchmarkReaderRead(b *testing.B) {
	d := &errorDecompressor{}
	r := NewReader(bytes.NewReader(nil), func(io.Reader) Decompressor { return d })
	p := make([]byte, 1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Read(p); err != nil {
			b.Fatal(err)
		}
	}
}

type resetErrorDecompressor struct {
	errorDecompressor
}

func (d *resetErrorDecompressor) Reset(io.Reader) {
	d.reads = 0
	d.readErr = nil
}

func TestReaderResetClearsReadErrorWhenReusingDecompressor(t *testing.T) {
	d := &resetErrorDecompressor{errorDecompressor: errorDecompressor{readErr: io.ErrUnexpectedEOF}}
	constructions := 0
	r := NewReader(bytes.NewReader(nil), func(io.Reader) Decompressor {
		constructions++
		return d
	})
	p := make([]byte, 1)
	_, _ = r.Read(p)
	r.Reset(bytes.NewReader(nil))
	if n, err := r.Read(p); n != 1 || err != nil || constructions != 1 || r.Err() != nil {
		t.Fatalf(
			"Read() after Reset() = %d, %v; constructions=%d, saved error=%v",
			n, err, constructions, r.Err(),
		)
	}
}

func BenchmarkReaderDecompress(b *testing.B) {
	data := bytes.Repeat([]byte("hello compressed websocket message "), 1024)
	var compressed bytes.Buffer
	w, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		b.Fatal(err)
	}
	_, _ = w.Write(data)
	_ = w.Flush()
	payload := compressed.Bytes()[:compressed.Len()-4]
	r := NewReader(nil, func(src io.Reader) Decompressor { return flate.NewReader(src) })
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Reset(bytes.NewReader(payload))
		if _, err := io.Copy(io.Discard, r); err != nil {
			b.Fatal(err)
		}
		if err := r.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
