package ws

import (
	"bytes"
	"encoding/binary"
	"math/rand"
)

// Constants defined by specification.
const (
	// All control frames MUST have a payload length of 125 bytes or less and MUST NOT be fragmented.
	MaxControlFramePayloadSize = 125
)

// OpCode represents operation code.
type OpCode byte

// Operation codes defined by specification.
// See https://tools.ietf.org/html/rfc6455#section-5.2
const (
	OpContinuation OpCode = 0x0
	OpText                = 0x1
	OpBinary              = 0x2
	OpClose               = 0x8
	OpPing                = 0x9
	OpPong                = 0xa
)

// IsControl checks wheter the c is control operation code.
// See https://tools.ietf.org/html/rfc6455#section-5.5
func (c OpCode) IsControl() bool {
	// RFC6455: Control frames are identified by opcodes where
	// the most significant bit of the opcode is 1.
	//
	// Note that OpCode is only 4 bit length.
	return c&0x8 != 0
}

// IsData checks wheter the c is data operation code.
// See https://tools.ietf.org/html/rfc6455#section-5.6
func (c OpCode) IsData() bool {
	// RFC6455: Data frames (e.g., non-control frames) are identified by opcodes
	// where the most significant bit of the opcode is 0.
	//
	// Note that OpCode is only 4 bit length.
	return c&0x8 == 0
}

// IsReserved checks wheter the c is reserved operation code.
// See https://tools.ietf.org/html/rfc6455#section-5.2
func (c OpCode) IsReserved() bool {
	// RFC6455:
	// %x3-7 are reserved for further non-control frames
	// %xB-F are reserved for further control frames
	return (0x3 <= c && c <= 0x7) || (0xb <= c && c <= 0xf)
}

// StatusCode represents the encoded reason for closure of websocket connection.
//
// There are few helper methods on StatusCode that helps to define a range in
// which given code is lay in. accordingly to ranges defined in specification.
//
// See https://tools.ietf.org/html/rfc6455#section-7.4
type StatusCode uint16

// StatusCodeRange describes range of StatusCode values.
type StatusCodeRange struct {
	Min, Max StatusCode
}

// Status code ranges defined by specification.
// See https://tools.ietf.org/html/rfc6455#section-7.4.2
var (
	StatusRangeNotInUse    = StatusCodeRange{0, 999}
	StatusRangeProtocol    = StatusCodeRange{1000, 2999}
	StatusRangeApplication = StatusCodeRange{3000, 3999}
	StatusRangePrivate     = StatusCodeRange{4000, 4999}
)

// Status codes defined by specification.
// See https://tools.ietf.org/html/rfc6455#section-7.4.1
const (
	StatusNormalClosure           StatusCode = 1000
	StatusGoingAway                          = 1001
	StatusProtocolError                      = 1002
	StatusUnsupportedData                    = 1003
	StatusNoMeaningYet                       = 1004
	StatusNoStatusRcvd                       = 1005
	StatusAbnormalClosure                    = 1006
	StatusInvalidFramePayloadData            = 1007
	StatusPolicyViolation                    = 1008
	StatusMessageTooBig                      = 1009
	StatusMandatoryExt                       = 1010
	StatusInternalServerError                = 1011
	StatusTLSHandshake                       = 1015
)

// In reports whether the code is defined in given range.
func (s StatusCode) In(r StatusCodeRange) bool {
	return r.Min <= s && s <= r.Max
}

// Empty reports wheter the code is empty.
// Empty code has no any meaning neither app level codes nor other.
// This method is useful just to check that code is golang default value 0.
func (s StatusCode) Empty() bool {
	return s == 0
}

// IsNotUsed reports whether the code is predefined in not used range.
func (s StatusCode) IsNotUsed() bool {
	return s.In(StatusRangeNotInUse)
}

// IsApplicationSpec reports whether the code should be defined by
// application, framework or libraries specification.
func (s StatusCode) IsApplicationSpec() bool {
	return s.In(StatusRangeApplication)
}

// IsPrivateSpec reports whether the code should be defined privately.
func (s StatusCode) IsPrivateSpec() bool {
	return s.In(StatusRangePrivate)
}

// IsProtocolSpec reports whether the code should be defined by protocol specification.
func (s StatusCode) IsProtocolSpec() bool {
	return s.In(StatusRangeProtocol)
}

// IsProtocolDefined reports whether the code is already defined by protocol specification.
func (s StatusCode) IsProtocolDefined() bool {
	switch s {
	case StatusNormalClosure,
		StatusGoingAway,
		StatusProtocolError,
		StatusUnsupportedData,
		StatusInvalidFramePayloadData,
		StatusPolicyViolation,
		StatusMessageTooBig,
		StatusMandatoryExt,
		StatusInternalServerError,
		StatusNoStatusRcvd,
		StatusAbnormalClosure,
		StatusTLSHandshake:
		return true
	}
	return false
}

// IsProtocolReserved reports whether the code is defined by protocol specification
// to be reserved only for application usage purpose.
func (s StatusCode) IsProtocolReserved() bool {
	switch s {
	// [RFC6455]: {1005,1006,1015} is a reserved value and MUST NOT be set as a status code in a
	// Close control frame by an endpoint.
	case StatusNoStatusRcvd, StatusAbnormalClosure, StatusTLSHandshake:
		return true
	default:
		return false
	}
}

// Common frames with no special meaning.
var (
	PingFrame  = Frame{Header{Fin: true, OpCode: OpPing}, nil}
	PongFrame  = Frame{Header{Fin: true, OpCode: OpPong}, nil}
	CloseFrame = Frame{Header{Fin: true, OpCode: OpClose}, nil}
)

// Compiled control frames for common use cases.
// For construct-serialize optimizations.
var (
	CompiledPing  = MustCompileFrame(PingFrame)
	CompiledPong  = MustCompileFrame(PongFrame)
	CompiledClose = MustCompileFrame(CloseFrame)
)

// Header represents websocket frame header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
type Header struct {
	Fin    bool
	Rsv    byte
	OpCode OpCode
	Length int64
	Masked bool
	Mask   [4]byte
}

// Rsv1 reports whether the header has first rsv bit set.
func (h Header) Rsv1() bool { return h.Rsv&bit5 != 0 }

// Rsv2 reports whether the header has second rsv bit set.
func (h Header) Rsv2() bool { return h.Rsv&bit6 != 0 }

// Rsv3 reports whether the header has third rsv bit set.
func (h Header) Rsv3() bool { return h.Rsv&bit7 != 0 }

// Frame represents websocket frame.
// See https://tools.ietf.org/html/rfc6455#section-5.2
type Frame struct {
	Header  Header
	Payload []byte
}

// NewFrame creates frame with given operation code,
// flag of completeness and payload bytes.
func NewFrame(op OpCode, fin bool, p []byte) Frame {
	return Frame{
		Header: Header{
			Fin:    fin,
			OpCode: op,
			Length: int64(len(p)),
		},
		Payload: p,
	}
}

// NewTextFrame creates text frame with s as payload.
// Note that the s is copied in the returned frame payload.
func NewTextFrame(s string) Frame {
	p := make([]byte, len(s))
	copy(p, s)
	return NewFrame(OpText, true, p)
}

// NewBinaryFrame creates binary frame with p as payload.
// Note that p is left as is in the returned frame without copying.
func NewBinaryFrame(p []byte) Frame {
	return NewFrame(OpBinary, true, p)
}

// NewPingFrame creates ping frame with p as payload.
// Note that p is left as is in the returned frame without copying.
func NewPingFrame(p []byte) Frame {
	return NewFrame(OpPing, true, p)
}

// NewPongFrame creates pong frame with p as payload.
// Note that p is left as is in the returned frame.
func NewPongFrame(p []byte) Frame {
	return NewFrame(OpPong, true, p)
}

// NewCloseFrame creates close frame with given closure code and reason.
// Note that it crops reason to fit the limit of control frames payload.
// See https://tools.ietf.org/html/rfc6455#section-5.5
func NewCloseFrame(code StatusCode, reason string) Frame {
	return NewFrame(OpClose, true, NewCloseFrameData(code, reason))
}

// NewCloseFrameData makes byte representation of code and reason.
//
// Note that returned slice is at most 125 bytes length.
// If reason is too big it will crop it to fit the limit defined by thte spec.
//
// See https://tools.ietf.org/html/rfc6455#section-5.5
func NewCloseFrameData(code StatusCode, reason string) []byte {
	n := min(2+len(reason), MaxControlFramePayloadSize) // 2 is for status code uint16 encoding.
	p := make([]byte, n)
	PutCloseFrameData(p, code, reason)
	return p
}

// PutCloseFrameData encodes code and reason into buf and returns the number of bytes written.
// If the buffer is too small to accommodate at least code, PutCloseFrameData will panic.
// Note that it does not checks maximum control frame payload size limit.
func PutCloseFrameData(p []byte, code StatusCode, reason string) int {
	binary.BigEndian.PutUint16(p, uint16(code))
	n := copy(p[2:], reason)
	return n + 2
}

// MaskFrame masks frame and returns frame with masked payload and Mask header's field set.
// Note that it copies f payload to prevent collisions.
// For less allocations you could use MaskFrameInPlace or construct frame manually.
func MaskFrame(f Frame) Frame {
	return MaskFrameWith(f, NewMask())
}

// MaskFrameWith masks frame with given mask and returns frame
// with masked payload and Mask header's field set.
// Note that it copies f payload to prevent collisions.
// For less allocations you could use MaskFrameInPlaceWith or construct frame manually.
func MaskFrameWith(f Frame, mask [4]byte) Frame {
	// TODO(gobwas): check CopyCipher ws copy() Cipher().
	p := make([]byte, len(f.Payload))
	copy(p, f.Payload)
	f.Payload = p
	return MaskFrameInPlaceWith(f, mask)
}

// MaskFrame masks frame and returns frame with masked payload and Mask header's field set.
// Note that it applies xor cipher to f.Payload without copying, that is, it modifies f.Payload inplace.
func MaskFrameInPlace(f Frame) Frame {
	return MaskFrameInPlaceWith(f, NewMask())
}

// MaskFrameInPlaceWith masks frame with given mask and returns frame
// with masked payload and Mask header's field set.
// Note that it applies xor cipher to f.Payload without copying, that is, it modifies f.Payload inplace.
func MaskFrameInPlaceWith(f Frame, m [4]byte) Frame {
	f.Header.Masked = true
	f.Header.Mask = m
	Cipher(f.Payload, m, 0)
	return f
}

// NewMask creates new random mask.
func NewMask() (ret [4]byte) {
	binary.BigEndian.PutUint32(ret[:], rand.Uint32())
	return
}

// CompileFrame returns byte representation of given frame.
// In terms of memory consumption it is useful to precompile static frames which are often used.
func CompileFrame(f Frame) (bts []byte, err error) {
	buf := bytes.NewBuffer(make([]byte, 0, 16))
	err = WriteFrame(buf, f)
	bts = buf.Bytes()
	return
}

// MustCompileFrame is like CompileFrame but panics if frame cannot be encoded.
func MustCompileFrame(f Frame) []byte {
	bts, err := CompileFrame(f)
	if err != nil {
		panic(err)
	}
	return bts
}

// Rsv creates rsv byte representation.
func Rsv(r1, r2, r3 bool) (rsv byte) {
	if r1 {
		rsv |= bit5
	}
	if r2 {
		rsv |= bit6
	}
	if r3 {
		rsv |= bit7
	}
	return rsv
}
