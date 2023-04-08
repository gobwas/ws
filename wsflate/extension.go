package wsflate

import (
	"bytes"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
)

// Extension contains logic of compression extension parameters negotiation
// made during HTTP WebSocket handshake.
// It might be reused between different upgrades (but not concurrently) with
// Reset() being called after each.
type Extension struct {
	// Parameters is specification of extension parameters server is going to
	// accept.
	Parameters Parameters

	accepted bool
	params   Parameters
}

// Negotiate parses given HTTP header option and returns (if any) header option
// which describes accepted parameters.
//
// It may return zero option (i.e. one which Size() returns 0) alongside with
// nil error.
func (n *Extension) Negotiate(opt httphead.Option) (accept httphead.Option, err error) {
	if !bytes.Equal(opt.Name, ExtensionNameBytes) {
		return accept, nil
	}
	if n.accepted {
		// Negotiate might be called multiple times during upgrade.
		// We stick to first one accepted extension since they must be passed
		// in ordered by preference.
		return accept, nil
	}

	want := n.Parameters

	// NOTE: Parse() resets params inside, so no worries.
	if err := n.params.Parse(opt); err != nil {
		return accept, err
	}
	{
		offer := n.params.ServerMaxWindowBits
		want := want.ServerMaxWindowBits
		if offer > want {
			// A server declines an extension negotiation offer
			// with this parameter if the server doesn't support
			// it.
			return accept, nil
		}
	}
	{
		// If a received extension negotiation offer has the
		// "client_max_window_bits" extension parameter, the server MAY
		// include the "client_max_window_bits" extension parameter in the
		// corresponding extension negotiation response to the offer.
		offer := n.params.ClientMaxWindowBits
		want := want.ClientMaxWindowBits
		if want > offer {
			return accept, nil
		}
	}
	{
		offer := n.params.ServerNoContextTakeover
		want := want.ServerNoContextTakeover
		if offer && !want {
			return accept, nil
		}
	}

	n.accepted = true

	return want.Option(), nil
}

// Accepted returns parameters parsed during last negotiation and a flag that
// reports whether they were accepted.
func (n *Extension) Accepted() (_ Parameters, accepted bool) {
	return n.params, n.accepted
}

// Reset resets extension for further reuse.
func (n *Extension) Reset() {
	n.accepted = false
	n.params = Parameters{}
}

var ErrUnexpectedCompressionBit = ws.ProtocolError(
	"control frame or non-first fragment of data contains compression bit set",
)

// UnsetBit clears the Per-Message Compression bit in header h and returns its
// modified copy. It reports whether compression bit was set in header h.
// It returns non-nil error if compression bit has unexpected value.
//
// This function's main purpose is to be compatible with "Framing" section of
// the Compression Extensions for WebSocket RFC. If you don't need to work with
// chains of extensions then IsCompressed() could be enough to check if
// message is compressed.
// See https://tools.ietf.org/html/rfc7692#section-6.2
func UnsetBit(h ws.Header) (_ ws.Header, wasSet bool, err error) {
	var s MessageState
	h, err = s.UnsetBits(h)
	return h, s.IsCompressed(), err
}

// SetBit sets the Per-Message Compression bit in header h and returns its
// modified copy.
// It returns non-nil error if compression bit has unexpected value.
func SetBit(h ws.Header) (_ ws.Header, err error) {
	var s MessageState
	s.SetCompressed(true)
	return s.SetBits(h)
}

// IsCompressed reports whether the Per-Message Compression bit is set in
// header h.
// It returns non-nil error if compression bit has unexpected value.
//
// If you need to be fully compatible with Compression Extensions for WebSocket
// RFC and work with chains of extensions, take a look at the UnsetBit()
// instead. That is, IsCompressed() is a shortcut for UnsetBit() with reduced
// number of return values.
func IsCompressed(h ws.Header) (bool, error) {
	_, isSet, err := UnsetBit(h)
	return isSet, err
}

// MessageState holds message compression state.
//
// It is consulted during SetBits(h) call to make a decision whether we must
// set the Per-Message Compression bit for given header h argument.
// It is updated during UnsetBits(h) to reflect compression state of a message
// represented by header h argument.
// It can also be consulted/updated directly by calling
// IsCompressed()/SetCompressed().
//
// In general MessageState should be used when there is no direct access to
// connection to read frame from, but it is still needed to know if message
// being read is compressed. For other cases SetBit() and UnsetBit() should be
// used instead.
//
// NOTE: the compression state is updated during UnsetBits(h) only when header
// h argument represents data (text or binary) frame.
type MessageState struct {
	compressed bool
}

// SetCompressed marks message as "compressed" or "uncompressed".
// See https://tools.ietf.org/html/rfc7692#section-6
func (s *MessageState) SetCompressed(v bool) {
	s.compressed = v
}

// IsCompressed reports whether message is "compressed".
// See https://tools.ietf.org/html/rfc7692#section-6
func (s *MessageState) IsCompressed() bool {
	return s.compressed
}

// UnsetBits changes RSV bits of the given frame header h as if compression
// extension was negotiated. It returns modified copy of h and error if header
// is malformed from the RFC perspective.
func (s *MessageState) UnsetBits(h ws.Header) (ws.Header, error) {
	r1, r2, r3 := ws.RsvBits(h.Rsv)
	switch {
	case h.OpCode.IsData() && h.OpCode != ws.OpContinuation:
		h.Rsv = ws.Rsv(false, r2, r3)
		s.SetCompressed(r1)
		return h, nil

	case r1:
		// An endpoint MUST NOT set the "Per-Message Compressed"
		// bit of control frames and non-first fragments of a data
		// message. An endpoint receiving such a frame MUST _Fail
		// the WebSocket Connection_.
		return h, ErrUnexpectedCompressionBit

	default:
		// NOTE: do not change the state of s.compressed since UnsetBits()
		// might also be called for (intermediate) control frames.
		return h, nil
	}
}

// SetBits changes RSV bits of the frame header h which is being send as if
// compression extension was negotiated. It returns modified copy of h and
// error if header is malformed from the RFC perspective.
func (s *MessageState) SetBits(h ws.Header) (ws.Header, error) {
	r1, r2, r3 := ws.RsvBits(h.Rsv)
	if r1 {
		return h, ErrUnexpectedCompressionBit
	}
	if !h.OpCode.IsData() || h.OpCode == ws.OpContinuation {
		// An endpoint MUST NOT set the "Per-Message Compressed"
		// bit of control frames and non-first fragments of a data
		// message. An endpoint receiving such a frame MUST _Fail
		// the WebSocket Connection_.
		return h, nil
	}
	if s.IsCompressed() {
		h.Rsv = ws.Rsv(true, r2, r3)
	}
	return h, nil
}
