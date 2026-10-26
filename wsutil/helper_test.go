package wsutil

import (
	"bytes"
	"io"
	"testing"

	"github.com/gobwas/ws"
)

func TestReadMessageEOF(t *testing.T) {
	for _, test := range []struct {
		source   func() io.Reader
		messages []Message
		err      error
	}{
		{
			source: func() io.Reader { return eofReader },
			err:    io.EOF,
		},
		{
			source: func() io.Reader {
				// This case tests that ReadMessage still fails after
				// successfully reading header bytes frame via ws.ReadHeader()
				// and non-successfully read of the body.
				var buf bytes.Buffer
				f := ws.NewTextFrame([]byte("this part will be lost"))
				if err := ws.WriteHeader(&buf, f.Header); err != nil {
					t.Fatal(err)
				}
				return &buf
			},
			err: io.ErrUnexpectedEOF,
		},
		{
			source: func() io.Reader {
				// This case tests that ReadMessage not fail when reading
				// fragmented messages.
				var buf bytes.Buffer
				fs := []ws.Frame{
					ws.NewFrame(ws.OpText, false, []byte("fragment1")),
					ws.NewFrame(ws.OpContinuation, false, []byte(",")),
					ws.NewFrame(ws.OpContinuation, true, []byte("fragment2")),
				}
				for _, f := range fs {
					if err := ws.WriteFrame(&buf, f); err != nil {
						t.Fatal(err)
					}
				}
				return &buf
			},
			messages: []Message{
				{ws.OpText, []byte("fragment1,fragment2")},
			},
		},
	} {
		t.Run("", func(t *testing.T) {
			ms, err := ReadMessage(test.source(), 0, nil)
			if err != test.err {
				t.Errorf(
					"unexpected error: %v; want %v",
					err, test.err,
				)
			}
			if n := len(ms); n != len(test.messages) {
				t.Fatalf(
					"unexpected number of read messages: %d; want %d",
					n, 0,
				)
			}
			for i, exp := range test.messages {
				act := ms[i]
				if act.OpCode != exp.OpCode {
					t.Errorf(
						"unexpected #%d message op code: %v; want %v",
						i, act.OpCode, exp.OpCode,
					)
				}
				if !bytes.Equal(act.Payload, exp.Payload) {
					t.Errorf(
						"unexpected #%d message payload: %q; want %q",
						i, string(act.Payload), string(exp.Payload),
					)
				}
			}
		})
	}
}

func TestReadMessageDeclaredLength(t *testing.T) {
	// Peer-declared payload length must not be trusted for allocation: with
	// no payload bytes available ReadMessage() must fail without allocating
	// more than defaultPreallocLimit.
	for _, length := range []int64{
		defaultPreallocLimit + 1,
		1 << 34, // 16 GiB
		1<<63 - 1,
	} {
		var buf bytes.Buffer
		err := ws.WriteHeader(&buf, ws.Header{
			Fin:    true,
			OpCode: ws.OpBinary,
			Length: length,
		})
		if err != nil {
			t.Fatalf(
				"unexpected WriteHeader() error: %v",
				err,
			)
		}
		ms, err := ReadMessage(&buf, 0, nil)
		if err != io.ErrUnexpectedEOF {
			t.Fatalf(
				"len=%d: unexpected ReadMessage() error: %v; want %v",
				length, err, io.ErrUnexpectedEOF,
			)
		}
		if len(ms) != 0 {
			t.Fatalf(
				"len=%d: ReadMessage() returned %d messages; want 0",
				length, len(ms),
			)
		}
	}
}

func TestReadMessagePayloadGrowth(t *testing.T) {
	lim := int(defaultPreallocLimit)
	for _, size := range []int{
		0,
		1,
		lim,
		3*lim + 7,
	} {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}
		raw := ws.MustCompileFrame(ws.NewBinaryFrame(payload))

		ms, err := ReadMessage(bytes.NewReader(raw), 0, nil)
		if err != nil {
			t.Fatalf(
				"size=%d: unexpected ReadMessage() error: %v",
				size, err,
			)
		}
		if len(ms) != 1 {
			t.Fatalf(
				"size=%d: ReadMessage() returned %d messages; want 1",
				size, len(ms),
			)
		}
		if !bytes.Equal(ms[0].Payload, payload) {
			t.Fatalf(
				"size=%d: unexpected payload: %d bytes; want %d",
				size, len(ms[0].Payload), len(payload),
			)
		}
		if size > 0 && cap(ms[0].Payload) != size {
			t.Fatalf(
				"size=%d: payload capacity is %d; want exactly %d",
				size, cap(ms[0].Payload), size,
			)
		}
	}
}

func TestReadDataDeclaredLength(t *testing.T) {
	// Same as TestReadMessageDeclaredLength, but for the ReadData() family,
	// which reads the payload through the same path.
	for _, length := range []int64{
		defaultPreallocLimit + 1,
		1 << 34, // 16 GiB
		1<<63 - 1,
	} {
		var buf bytes.Buffer
		err := ws.WriteHeader(&buf, ws.Header{
			Fin:    true,
			OpCode: ws.OpBinary,
			Length: length,
		})
		if err != nil {
			t.Fatalf(
				"unexpected WriteHeader() error: %v",
				err,
			)
		}
		p, _, err := ReadData(&buf, ws.StateClientSide)
		if err != io.ErrUnexpectedEOF {
			t.Fatalf(
				"len=%d: unexpected ReadData() error: %v; want %v",
				length, err, io.ErrUnexpectedEOF,
			)
		}
		if n := int64(cap(p)); n > defaultPreallocLimit {
			t.Fatalf(
				"len=%d: ReadData() allocated %d bytes; want at most %d",
				length, n, defaultPreallocLimit,
			)
		}
	}
}

func TestSetMaxFrameSize(t *testing.T) {
	SetMaxFrameSize(16)
	defer SetMaxFrameSize(0)

	raw := ws.MustCompileFrame(ws.NewBinaryFrame(make([]byte, 17)))
	if _, err := ReadMessage(bytes.NewReader(raw), 0, nil); err != ErrFrameTooLarge {
		t.Fatalf(
			"unexpected ReadMessage() error: %v; want %v",
			err, ErrFrameTooLarge,
		)
	}
	var buf bytes.Buffer
	buf.Write(raw)
	if _, _, err := ReadData(&buf, ws.StateClientSide); err != ErrFrameTooLarge {
		t.Fatalf(
			"unexpected ReadData() error: %v; want %v",
			err, ErrFrameTooLarge,
		)
	}

	raw = ws.MustCompileFrame(ws.NewBinaryFrame(make([]byte, 16)))
	if _, err := ReadMessage(bytes.NewReader(raw), 0, nil); err != nil {
		t.Fatalf(
			"unexpected ReadMessage() error: %v",
			err,
		)
	}
}

func TestSetMaxFrameSizeAllocation(t *testing.T) {
	// A bound above the prealloc limit does not raise what the declared
	// length is trusted for: with no payload bytes available the allocation
	// is still bounded by the prealloc limit.
	size := 3*int64(defaultPreallocLimit) + 7
	SetMaxFrameSize(size)
	defer SetMaxFrameSize(0)

	var buf bytes.Buffer
	err := ws.WriteHeader(&buf, ws.Header{
		Fin:    true,
		OpCode: ws.OpBinary,
		Length: size,
	})
	if err != nil {
		t.Fatalf(
			"unexpected WriteHeader() error: %v",
			err,
		)
	}
	p, _, err := ReadData(&buf, ws.StateClientSide)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf(
			"unexpected ReadData() error: %v; want %v",
			err, io.ErrUnexpectedEOF,
		)
	}
	if n := cap(p); n > defaultPreallocLimit {
		t.Fatalf(
			"ReadData() allocated %d bytes; want at most %d",
			n, defaultPreallocLimit,
		)
	}

	// A complete payload is still read whole and exactly.
	raw := ws.MustCompileFrame(ws.NewBinaryFrame(make([]byte, size)))
	ms, err := ReadMessage(bytes.NewReader(raw), 0, nil)
	if err != nil {
		t.Fatalf(
			"unexpected ReadMessage() error: %v",
			err,
		)
	}
	if n := int64(cap(ms[0].Payload)); n != size {
		t.Fatalf(
			"payload capacity is %d; want exactly %d",
			n, size,
		)
	}
}

func TestSetPreallocLimit(t *testing.T) {
	SetPreallocLimit(8)
	defer SetPreallocLimit(defaultPreallocLimit)

	// With no payload bytes available the allocation is bounded by the limit.
	var buf bytes.Buffer
	err := ws.WriteHeader(&buf, ws.Header{
		Fin:    true,
		OpCode: ws.OpBinary,
		Length: 1 << 40,
	})
	if err != nil {
		t.Fatalf(
			"unexpected WriteHeader() error: %v",
			err,
		)
	}
	p, _, err := ReadData(&buf, ws.StateClientSide)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf(
			"unexpected ReadData() error: %v; want %v",
			err, io.ErrUnexpectedEOF,
		)
	}
	if n := cap(p); n > 8 {
		t.Fatalf(
			"ReadData() allocated %d bytes; want at most 8",
			n,
		)
	}

	// A complete payload is still read whole and exactly.
	data := make([]byte, 1024)
	raw := ws.MustCompileFrame(ws.NewBinaryFrame(data))
	ms, err := ReadMessage(bytes.NewReader(raw), 0, nil)
	if err != nil {
		t.Fatalf(
			"unexpected ReadMessage() error: %v",
			err,
		)
	}
	if n := cap(ms[0].Payload); n != len(data) {
		t.Fatalf(
			"payload capacity is %d; want exactly %d",
			n, len(data),
		)
	}
}

func TestSetPreallocLimitPanics(t *testing.T) {
	for _, n := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf(
						"SetPreallocLimit(%d) did not panic",
						n,
					)
				}
			}()
			SetPreallocLimit(n)
		}()
	}
}

func TestSettingsGetters(t *testing.T) {
	defer SetMaxFrameSize(MaxFrameSize())
	defer SetMaxMessageSize(MaxMessageSize())
	defer SetCheckUTF8(CheckUTF8())
	defer SetPreallocLimit(PreallocLimit())

	if v := MaxFrameSize(); v != 0 {
		t.Fatalf(
			"MaxFrameSize() default: %d; want 0",
			v,
		)
	}
	if v := MaxMessageSize(); v != 0 {
		t.Fatalf(
			"MaxMessageSize() default: %d; want 0",
			v,
		)
	}
	if v := CheckUTF8(); !v {
		t.Fatalf(
			"CheckUTF8() default: %t; want true",
			v,
		)
	}
	if v := PreallocLimit(); v != defaultPreallocLimit {
		t.Fatalf(
			"PreallocLimit() default: %d; want %d",
			v, defaultPreallocLimit,
		)
	}

	SetMaxFrameSize(42)
	SetMaxMessageSize(84)
	SetCheckUTF8(false)
	SetPreallocLimit(7)

	if v := MaxFrameSize(); v != 42 {
		t.Fatalf(
			"MaxFrameSize(): %d; want 42",
			v,
		)
	}
	if v := MaxMessageSize(); v != 84 {
		t.Fatalf(
			"MaxMessageSize(): %d; want 84",
			v,
		)
	}
	if v := CheckUTF8(); v {
		t.Fatalf(
			"CheckUTF8(): %t; want false",
			v,
		)
	}
	if v := PreallocLimit(); v != 7 {
		t.Fatalf(
			"PreallocLimit(): %d; want 7",
			v,
		)
	}

	src := bytes.NewReader(nil)
	rd := DefaultReader(src, ws.StateServerSide)
	if rd.Source != src || rd.State != ws.StateServerSide {
		t.Fatalf(
			"DefaultReader(): Source=%v State=%v; want the given ones",
			rd.Source, rd.State,
		)
	}
	if rd.MaxFrameSize != 42 || rd.MaxMessageSize != 84 || rd.CheckUTF8 {
		t.Fatalf(
			"DefaultReader(): MaxFrameSize=%d MaxMessageSize=%d CheckUTF8=%t; want 42 84 false",
			rd.MaxFrameSize, rd.MaxMessageSize, rd.CheckUTF8,
		)
	}
	if rd.OnIntermediate != nil {
		t.Fatalf(
			"DefaultReader(): OnIntermediate is set; want nil",
		)
	}
}

func TestSetMaxMessageSize(t *testing.T) {
	SetMaxMessageSize(16)
	defer SetMaxMessageSize(0)

	// frames writes fragments of a message with the given payload sizes.
	frames := func(sizes ...int) []byte {
		var buf bytes.Buffer
		for i, n := range sizes {
			op := ws.OpContinuation
			if i == 0 {
				op = ws.OpBinary
			}
			f := ws.NewFrame(op, i == len(sizes)-1, make([]byte, n))
			err := ws.WriteFrame(&buf, f)
			if err != nil {
				t.Fatal(err)
			}
		}
		return buf.Bytes()
	}

	_, err := ReadMessage(bytes.NewReader(frames(10, 10)), 0, nil)
	if err != ErrMessageTooLarge {
		t.Fatalf(
			"ReadMessage() error: %v; want %v",
			err, ErrMessageTooLarge,
		)
	}
	buf := bytes.NewBuffer(frames(10, 10))
	_, _, err = ReadData(buf, ws.StateClientSide)
	if err != ErrMessageTooLarge {
		t.Fatalf(
			"ReadData() error: %v; want %v",
			err, ErrMessageTooLarge,
		)
	}
	ms, err := ReadMessage(bytes.NewReader(frames(8, 8)), 0, nil)
	if err != nil {
		t.Fatalf(
			"ReadMessage() error: %v; want nil",
			err,
		)
	}
	if n := len(ms[0].Payload); n != 16 {
		t.Fatalf(
			"ReadMessage() payload is %d bytes; want 16",
			n,
		)
	}
}

func TestSetMaxMessageSizeAllocation(t *testing.T) {
	// Same as for MaxFrameSize: the bound rejects, it does not raise trust.
	size := 3*int64(defaultPreallocLimit) + 7
	SetMaxMessageSize(size)
	defer SetMaxMessageSize(0)

	var buf bytes.Buffer
	err := ws.WriteHeader(&buf, ws.Header{
		Fin:    true,
		OpCode: ws.OpBinary,
		Length: size,
	})
	if err != nil {
		t.Fatalf(
			"unexpected WriteHeader() error: %v",
			err,
		)
	}
	p, _, err := ReadData(&buf, ws.StateClientSide)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf(
			"unexpected ReadData() error: %v; want %v",
			err, io.ErrUnexpectedEOF,
		)
	}
	if n := cap(p); n > defaultPreallocLimit {
		t.Fatalf(
			"ReadData() allocated %d bytes; want at most %d",
			n, defaultPreallocLimit,
		)
	}
}

func TestReadMessageFragmentedDeclaredLength(t *testing.T) {
	// The first fragment's declared length is only a hint for the buffer
	// start size: with no payload bytes available the allocation is bounded
	// by the prealloc limit.
	var buf bytes.Buffer
	err := ws.WriteHeader(&buf, ws.Header{
		Fin:    false,
		OpCode: ws.OpBinary,
		Length: 1 << 40,
	})
	if err != nil {
		t.Fatalf(
			"unexpected WriteHeader() error: %v",
			err,
		)
	}
	p, _, err := ReadData(&buf, ws.StateClientSide)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf(
			"unexpected ReadData() error: %v; want %v",
			err, io.ErrUnexpectedEOF,
		)
	}
	if n := cap(p); n > defaultPreallocLimit {
		t.Fatalf(
			"ReadData() allocated %d bytes; want at most %d",
			n, defaultPreallocLimit,
		)
	}
}
