package ws

import (
	"encoding/binary"
	"io"
)

const (
	bit0 = 0x80
	bit1 = 0x40
	bit2 = 0x20
	bit3 = 0x10
	bit4 = 0x08
	bit5 = 0x04
	bit6 = 0x02
	bit7 = 0x01

	len16 = int64(^(uint16(0)))
	len64 = int64(^(uint64(0)) >> 1)
)

func WriteHeader(w io.Writer, h Header) error {
	size := 2

	var lenByte byte
	switch {
	case h.Length < 126:
		lenByte = byte(h.Length)
		size += 0

	case h.Length <= len16:
		lenByte = 126
		size += 2

	case h.Length <= len64:
		lenByte = 127
		size += 8

	default:
		return ErrHeaderLengthUnexpected
	}

	if h.Mask != nil {
		lenByte |= bit0
		size += 4
	}

	bts := make([]byte, size)

	if h.Fin {
		bts[0] |= bit0
	}
	bts[0] |= h.Rsv << 4
	bts[0] |= byte(h.OpCode)
	bts[1] = lenByte

	maskPos := 2 // after fin, rsv and op code byte and length byte.
	switch {
	case lenByte == 126:
		binary.BigEndian.PutUint16(bts[2:], uint16(h.Length))
		maskPos += 2
	case lenByte == 127:
		binary.BigEndian.PutUint64(bts[2:], uint64(h.Length))
		maskPos += 8
	}

	if h.Mask != nil {
		copy(bts[maskPos:], h.Mask)
	}

	_, err := w.Write(bts)
	return err
}

func WriteFrame(w io.Writer, f Frame) error {
	err := WriteHeader(w, f.Header)
	if err != nil {
		return err
	}
	_, err = w.Write(f.Payload)
	return err
}
