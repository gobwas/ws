package ws

import (
	"fmt"
	"unicode/utf8"
)

// State represents state of websocket endpoint.
// It used by some functions to be more strict when checking compatibility with RFC6455.
type State uint8

const (
	// StateServerSide means that endpoint (caller) is a server.
	StateServerSide State = 0x1 << iota
	// StateServerSide means that endpoint (caller) is a client.
	StateClientSide
	// StateExtended means that extension was negotiated during handshake.
	StateExtended
	// StateFragmented means that endpoint (caller) has received fragmented
	// frame and waits for continuation parts.
	StateFragmented
)

// Is checks whether the s has v enabled.
func (s State) Is(v State) bool {
	return uint8(s)&uint8(v) != 0
}

// Set enables v state on s.
func (s State) Set(v State) State {
	return s | v
}

// Clear disables v state on s.
func (s State) Clear(v State) State {
	return s & (^v)
}

// SetOrClearIf enables or disables v state on s depending on cond.
func (s State) SetOrClearIf(cond bool, v State) (ret State) {
	if cond {
		ret = s.Set(v)
	} else {
		ret = s.Clear(v)
	}
	return
}

// ProtocolError describes error during checking/parsing websocket frames or headers.
type ProtocolError error

// Errors used by the protocol checkers.
var (
	ErrProtocolOpCodeReserved             = ProtocolError(fmt.Errorf("use of reserved op code"))
	ErrProtocolControlPayloadOverflow     = ProtocolError(fmt.Errorf("control frame payload limit exceeded"))
	ErrProtocolControlNotFinal            = ProtocolError(fmt.Errorf("control frame is not final"))
	ErrProtocolNonZeroRsv                 = ProtocolError(fmt.Errorf("non-zero rsv bits with no extension negotiated"))
	ErrProtocolMaskRequired               = ProtocolError(fmt.Errorf("frames from client to server must be masked"))
	ErrProtocolMaskUnexpected             = ProtocolError(fmt.Errorf("frames from server to client must be not masked"))
	ErrProtocolContinuationExpected       = ProtocolError(fmt.Errorf("unexpected non-continuation data frame"))
	ErrProtocolContinuationUnexpected     = ProtocolError(fmt.Errorf("unexpected continuation data frame"))
	ErrProtocolStatusCodeNotInUse         = ProtocolError(fmt.Errorf("status code is not in use"))
	ErrProtocolStatusCodeApplicationLevel = ProtocolError(fmt.Errorf("status code is only application level"))
	ErrProtocolStatusCodeNoMeaning        = ProtocolError(fmt.Errorf("status code has no meaning yet"))
	ErrProtocolStatusCodeUnknown          = ProtocolError(fmt.Errorf("status code is not defined in spec"))
	ErrProtocolInvalidUTF8                = ProtocolError(fmt.Errorf("invalid utf8 sequence in close reason"))
)

// CheckHeader checks h to contain valid header data for given state s.
//
// Note that zero state (0) means that state is clean,
// neither server or client side, nor fragmented, nor extended.
func CheckHeader(h Header, s State) error {
	if h.OpCode.IsReserved() {
		return ErrProtocolOpCodeReserved
	}
	if h.OpCode.IsControl() {
		if h.Length > MaxControlFramePayloadSize {
			return ErrProtocolControlPayloadOverflow
		}
		if !h.Fin {
			return ErrProtocolControlNotFinal
		}
	}

	switch {
	// [RFC6455]: MUST be 0 unless an extension is negotiated that defines meanings for
	// non-zero values. If a nonzero value is received and none of the
	// negotiated extensions defines the meaning of such a nonzero value, the
	// receiving endpoint MUST _Fail the WebSocket Connection_.
	case h.Rsv != 0 && !s.Is(StateExtended):
		return ErrProtocolNonZeroRsv

	// [RFC6455]: The server MUST close the connection upon receiving a frame that is not masked.
	// In this case, a server MAY send a Close frame with a status code of 1002 (protocol error)
	// as defined in Section 7.4.1. A server MUST NOT mask any frames that it sends to the client.
	// A client MUST close a connection if it detects a masked frame. In this case, it MAY use the
	// status code 1002 (protocol error) as defined in Section 7.4.1.
	case s.Is(StateServerSide) && !h.Masked:
		return ErrProtocolMaskRequired
	case s.Is(StateClientSide) && h.Masked:
		return ErrProtocolMaskUnexpected

	// [RFC6455]: See detailed explanation in 5.4 section.
	case s.Is(StateFragmented) && !h.OpCode.IsControl() && h.OpCode != OpContinuation:
		return ErrProtocolContinuationExpected
	case !s.Is(StateFragmented) && h.OpCode == OpContinuation:
		return ErrProtocolContinuationUnexpected
	}

	return nil
}

// CheckCloseFrameData checks received close information
// to be valid RFC6455 compatible close info.
//
// Note that code.Empty() or code.IsAppLevel() will raise error.
//
// If endpoint sends close frame without status code (with frame.Length = 0),
// application should not check its payload.
func CheckCloseFrameData(code StatusCode, reason string) error {
	switch {
	case code.IsNotUsed():
		return ErrProtocolStatusCodeNotInUse

	case code.IsProtocolReserved():
		return ErrProtocolStatusCodeApplicationLevel

	case code == StatusNoMeaningYet:
		return ErrProtocolStatusCodeNoMeaning

	case code.IsProtocolSpec() && !code.IsProtocolDefined():
		return ErrProtocolStatusCodeUnknown

	case !utf8.ValidString(reason):
		return ErrProtocolInvalidUTF8

	default:
		return nil
	}
}
