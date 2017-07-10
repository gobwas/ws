package wsutil

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"testing"
	"unicode/utf8"

	. "github.com/gobwas/ws"
)

// TODO(gobwas): test continuation discard.
//				 test discard when NextFrame().

func TestReaderUTF8(t *testing.T) {
	yo := []byte("Ё")
	if !utf8.ValidString(string(yo)) {
		panic("bad fixture")
	}

	var buf bytes.Buffer
	WriteFrame(&buf,
		NewFrame(OpText, false, yo[:1]),
	)
	WriteFrame(&buf,
		NewFrame(OpContinuation, true, yo[1:]),
	)

	r := Reader{
		Source:    &buf,
		CheckUTF8: true,
	}

	bts, err := ioutil.ReadAll(&r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !bytes.Equal(bts, yo) {
		t.Errorf("ReadAll(r) = %v; want %v", bts, yo)
	}
}

func TestReader(t *testing.T) {
	for i, test := range []struct {
		label string
		seq   []Frame
		chop  int
		exp   []byte
		err   error
	}{
		{
			label: "empty",
			seq:   []Frame{},
			err:   io.EOF,
		},
		{
			label: "single",
			seq: []Frame{
				NewTextFrame("Привет, Мир!"),
			},
			exp: []byte("Привет, Мир!"),
		},
		{
			label: "single_masked",
			seq: []Frame{
				MaskFrame(NewTextFrame("Привет, Мир!")),
			},
			exp: []byte("Привет, Мир!"),
		},
		{
			label: "fragmented",
			seq: []Frame{
				NewFrame(OpText, false, []byte("Привет,")),
				NewFrame(OpContinuation, false, []byte(" о дивный,")),
				NewFrame(OpContinuation, false, []byte(" новый ")),
				NewFrame(OpContinuation, true, []byte("Мир!")),

				NewTextFrame("Hello, Brave New World!"),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
		{
			label: "fragmented_masked",
			seq: []Frame{
				MaskFrame(NewFrame(OpText, false, []byte("Привет,"))),
				MaskFrame(NewFrame(OpContinuation, false, []byte(" о дивный,"))),
				MaskFrame(NewFrame(OpContinuation, false, []byte(" новый "))),
				MaskFrame(NewFrame(OpContinuation, true, []byte("Мир!"))),

				MaskFrame(NewTextFrame("Hello, Brave New World!")),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
		{
			label: "fragmented_and_control",
			seq: []Frame{
				NewFrame(OpText, false, []byte("Привет,")),
				NewFrame(OpPing, true, nil),
				NewFrame(OpContinuation, false, []byte(" о дивный,")),
				NewFrame(OpPing, true, nil),
				NewFrame(OpContinuation, false, []byte(" новый ")),
				NewFrame(OpPing, true, nil),
				NewFrame(OpPing, true, []byte("ping info")),
				NewFrame(OpContinuation, true, []byte("Мир!")),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
		{
			label: "fragmented_and_control_mask",
			seq: []Frame{
				MaskFrame(NewFrame(OpText, false, []byte("Привет,"))),
				MaskFrame(NewFrame(OpPing, true, nil)),
				MaskFrame(NewFrame(OpContinuation, false, []byte(" о дивный,"))),
				MaskFrame(NewFrame(OpPing, true, nil)),
				MaskFrame(NewFrame(OpContinuation, false, []byte(" новый "))),
				MaskFrame(NewFrame(OpPing, true, nil)),
				MaskFrame(NewFrame(OpPing, true, []byte("ping info"))),
				MaskFrame(NewFrame(OpContinuation, true, []byte("Мир!"))),
			},
			exp: []byte("Привет, о дивный, новый Мир!"),
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			// Prepare input.
			buf := &bytes.Buffer{}
			for _, f := range test.seq {
				if err := WriteFrame(buf, f); err != nil {
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
