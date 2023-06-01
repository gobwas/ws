package wsutil

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"
	"unicode/utf8"

	"github.com/gobwas/ws"
)

// TODO(gobwas): test continuation discard.
//				 test discard when NextFrame().

var eofReader = bytes.NewReader(nil)

func TestReadFromWithIntermediateControl(t *testing.T) {
	var buf bytes.Buffer

	ws.MustWriteFrame(&buf, ws.NewFrame(ws.OpText, false, []byte("foo")))
	ws.MustWriteFrame(&buf, ws.NewPingFrame([]byte("ping")))
	ws.MustWriteFrame(&buf, ws.NewFrame(ws.OpContinuation, false, []byte("bar")))
	ws.MustWriteFrame(&buf, ws.NewPongFrame([]byte("pong")))
	ws.MustWriteFrame(&buf, ws.NewFrame(ws.OpContinuation, true, []byte("baz")))

	var intermediate [][]byte
	r := Reader{
		Source: &buf,
		OnIntermediate: func(h ws.Header, r io.Reader) error {
			bts, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			intermediate = append(
				intermediate,
				append([]byte(nil), bts...),
			)
			return nil
		},
	}

	h, err := r.NextFrame()
	if err != nil {
		t.Fatal(err)
	}
	exp := ws.Header{
		Length: 3,
		Fin:    false,
		OpCode: ws.OpText,
	}
	if act := h; act != exp {
		t.Fatalf("unexpected NextFrame() header: %+v; want %+v", act, exp)
	}

	act, err := ioutil.ReadAll(&r)
	if err != nil {
		t.Fatal(err)
	}
	if exp := []byte("foobarbaz"); !bytes.Equal(act, exp) {
		t.Errorf("unexpected all bytes: %q; want %q", act, exp)
	}
	if act, exp := len(intermediate), 2; act != exp {
		t.Errorf("unexpected intermediate payload: %d; want %d", act, exp)
	} else {
		for i, exp := range [][]byte{
			[]byte("ping"),
			[]byte("pong"),
		} {
			if act := intermediate[i]; !bytes.Equal(act, exp) {
				t.Errorf(
					"unexpected #%d intermediate payload: %q; want %q",
					i, act, exp,
				)
			}
		}
	}
}

func TestReaderNoFrameAdvance(t *testing.T) {
	r := Reader{
		Source: eofReader,
	}
	if _, err := r.Read(make([]byte, 10)); err != ErrNoFrameAdvance {
		t.Errorf("Read() returned %v; want %v", err, ErrNoFrameAdvance)
	}
}

func TestReaderNextFrameAndReadEOF(t *testing.T) {
	for _, test := range []struct {
		source       func() io.Reader
		nextFrameErr error
		readErr      error
	}{
		{
			source:       func() io.Reader { return eofReader },
			nextFrameErr: io.EOF,
			readErr:      ErrNoFrameAdvance,
		},
		{
			source: func() io.Reader {
				// This case tests that ReadMessage still fails after
				// successfully reading header bytes frame via ws.ReadHeader()
				// and non-successfully read of the body.
				var buf bytes.Buffer
				f := ws.NewTextFrame([]byte("this part will be lost"))
				if err := ws.WriteHeader(&buf, f.Header); err != nil {
					panic(err)
				}
				return &buf
			},
			nextFrameErr: nil,
			readErr:      io.ErrUnexpectedEOF,
		},
		{
			source: func() io.Reader {
				var buf bytes.Buffer
				f := ws.NewTextFrame([]byte("foobar"))
				if err := ws.WriteHeader(&buf, f.Header); err != nil {
					panic(err)
				}
				buf.WriteString("foo")
				return &buf
			},
			nextFrameErr: nil,
			readErr:      io.ErrUnexpectedEOF,
		},
		{
			source: func() io.Reader {
				var buf bytes.Buffer
				f := ws.NewFrame(ws.OpText, false, []byte("payload"))
				if err := ws.WriteFrame(&buf, f); err != nil {
					panic(err)
				}
				return &buf
			},
			nextFrameErr: nil,
			readErr:      io.ErrUnexpectedEOF,
		},
	} {
		t.Run("", func(t *testing.T) {
			r := Reader{
				Source: test.source(),
			}
			_, err := r.NextFrame()
			if err != test.nextFrameErr {
				t.Errorf("NextFrame() = %v; want %v", err, test.nextFrameErr)
			}
			var (
				p = make([]byte, 4096)
				i = 0
			)
			for {
				if i == 100 {
					t.Fatal(io.ErrNoProgress)
				}
				_, err := r.Read(p)
				if err == nil {
					continue
				}
				if err != test.readErr {
					t.Errorf("Read() = %v; want %v", err, test.readErr)
				}
				break
			}
		})
	}
}

func TestMaxFrameSize(t *testing.T) {
	var buf bytes.Buffer
	msg := []byte("small frame")
	f := ws.NewTextFrame(msg)
	if err := ws.WriteFrame(&buf, f); err != nil {
		t.Fatal(err)
	}
	r := Reader{
		Source:       &buf,
		MaxFrameSize: int64(len(msg)) - 1,
	}

	_, err := r.NextFrame()
	if got, want := err, ErrFrameTooLarge; got != want {
		t.Errorf("NextFrame() error = %v; want %v", got, want)
	}

	p := make([]byte, 100)
	n, err := r.Read(p)
	if got, want := err, ErrNoFrameAdvance; got != want {
		t.Errorf("Read() error = %v; want %v", got, want)
	}
	if got, want := n, 0; got != want {
		t.Errorf("Read() bytes returned = %v; want %v", got, want)
	}
}

func TestReaderUTF8(t *testing.T) {
	yo := []byte("Ё")
	if !utf8.ValidString(string(yo)) {
		panic("bad fixture")
	}

	var buf bytes.Buffer
	ws.WriteFrame(&buf,
		ws.NewFrame(ws.OpText, false, yo[:1]),
	)
	ws.WriteFrame(&buf,
		ws.NewFrame(ws.OpContinuation, true, yo[1:]),
	)

	r := Reader{
		Source:    &buf,
		CheckUTF8: true,
	}
	if _, err := r.NextFrame(); err != nil {
		t.Fatal(err)
	}
	bts, err := ioutil.ReadAll(&r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !bytes.Equal(bts, yo) {
		t.Errorf("ReadAll(r) = %v; want %v", bts, yo)
	}
}

func TestNextReader(t *testing.T) {
	for _, test := range []struct {
		name string
		seq  []ws.Frame
		chop int
		exp  []byte
		err  error
	}{
		{
			name: "empty",
			seq:  []ws.Frame{},
			err:  io.EOF,
		},
		{
			name: "single",
			seq: []ws.Frame{
				ws.NewTextFrame([]byte("Привет, Мир!")),
			},
			exp: []byte("Привет, Мир!"),
		},
		{
			name: "single_masked",
			seq: []ws.Frame{
				ws.MaskFrame(ws.NewTextFrame([]byte("Привет, Мир!"))),
			},
			exp: []byte("Привет, Мир!"),
		},
		{
			name: "fragmented",
			seq: []ws.Frame{
				ws.NewFrame(ws.OpText, false, []byte("Привет,")),
				ws.NewFrame(ws.OpContinuation, false, []byte(" о дивный,")),
				ws.NewFrame(ws.OpContinuation, false, []byte(" новый ")),
				ws.NewFrame(ws.OpContinuation, true, []byte("Мир!")),

				ws.NewTextFrame([]byte("Hello, Brave New World!")),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
		{
			name: "fragmented_masked",
			seq: []ws.Frame{
				ws.MaskFrame(ws.NewFrame(ws.OpText, false, []byte("Привет,"))),
				ws.MaskFrame(ws.NewFrame(ws.OpContinuation, false, []byte(" о дивный,"))),
				ws.MaskFrame(ws.NewFrame(ws.OpContinuation, false, []byte(" новый "))),
				ws.MaskFrame(ws.NewFrame(ws.OpContinuation, true, []byte("Мир!"))),

				ws.MaskFrame(ws.NewTextFrame([]byte("Hello, Brave New World!"))),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
		{
			name: "fragmented_and_control",
			seq: []ws.Frame{
				ws.NewFrame(ws.OpText, false, []byte("Привет,")),
				ws.NewFrame(ws.OpPing, true, nil),
				ws.NewFrame(ws.OpContinuation, false, []byte(" о дивный,")),
				ws.NewFrame(ws.OpPing, true, nil),
				ws.NewFrame(ws.OpContinuation, false, []byte(" новый ")),
				ws.NewFrame(ws.OpPing, true, nil),
				ws.NewFrame(ws.OpPing, true, []byte("ping info")),
				ws.NewFrame(ws.OpContinuation, true, []byte("Мир!")),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
		{
			name: "fragmented_and_control_mask",
			seq: []ws.Frame{
				ws.MaskFrame(ws.NewFrame(ws.OpText, false, []byte("Привет,"))),
				ws.MaskFrame(ws.NewFrame(ws.OpPing, true, nil)),
				ws.MaskFrame(ws.NewFrame(ws.OpContinuation, false, []byte(" о дивный,"))),
				ws.MaskFrame(ws.NewFrame(ws.OpPing, true, nil)),
				ws.MaskFrame(ws.NewFrame(ws.OpContinuation, false, []byte(" новый "))),
				ws.MaskFrame(ws.NewFrame(ws.OpPing, true, nil)),
				ws.MaskFrame(ws.NewFrame(ws.OpPing, true, []byte("ping info"))),
				ws.MaskFrame(ws.NewFrame(ws.OpContinuation, true, []byte("Мир!"))),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Prepare input.
			buf := &bytes.Buffer{}
			for _, f := range test.seq {
				if err := ws.WriteFrame(buf, f); err != nil {
					t.Fatal(err)
				}
			}

			conn := &chopReader{
				src: bytes.NewReader(buf.Bytes()),
				sz:  test.chop,
			}

			var bts []byte
			_, reader, err := NextReader(conn, 0)
			if err == nil {
				bts, err = ioutil.ReadAll(reader)
			}
			if err != test.err {
				t.Errorf("unexpected error; got %v; want %v", err, test.err)
				return
			}
			if test.err == nil && !bytes.Equal(bts, test.exp) {
				t.Errorf(
					"ReadAll from reader:\nact:\t%#x\nexp:\t%#x\nact:\t%s\nexp:\t%s\n",
					bts, test.exp, string(bts), string(test.exp),
				)
			}
		})
	}
}

type chopReader struct {
	src io.Reader
	sz  int
}

func (c chopReader) Read(p []byte) (n int, err error) {
	sz := c.sz
	if sz == 0 {
		sz = 1
	}
	if sz > len(p) {
		sz = len(p)
	}
	return c.src.Read(p[:sz])
}
