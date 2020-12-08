package wsflate

import (
	"bytes"
	"fmt"

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
		return
	}
	if n.accepted {
		// Negotiate might be called multiple times during upgrade.
		// We stick to first one accepted extension since they must be passed
		// in ordered by preference.
		return
	}

	want := n.Parameters

	// NOTE: Parse() resets params inside, so no worries.
	if err = n.params.Parse(opt); err != nil {
		return
	}
	{
		offer := n.params.ServerMaxWindowBits
		want := want.ServerMaxWindowBits
		if offer > want {
			// A server declines an extension negotiation offer
			// with this parameter if the server doesn't support
			// it.
			return
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
			return
		}
	}
	{
		offer := n.params.ServerNoContextTakeover
		want := want.ServerNoContextTakeover
		if offer && !want {
			return
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

var errNonFirstFragmentEnabledBit = ws.ProtocolError(
	"non-first fragment contains compression bit enabled",
)

// BitsRecv changes RSV bits of the received frame header as if compression
// extension was negotiated.
func BitsRecv(fseq int, rsv byte) (byte, error) {
	r1, r2, r3 := ws.RsvBits(rsv)
	if fseq > 0 && r1 {
		// An endpoint MUST NOT set the "Per-Message Compressed"
		// bit of control frames and non-first fragments of a data
		// message. An endpoint receiving such a frame MUST _Fail
		// the WebSocket Connection_.
		return rsv, errNonFirstFragmentEnabledBit
	}
	if fseq > 0 {
		return rsv, nil
	}
	return ws.Rsv(false, r2, r3), nil
}

// BitsSend changes RSV bits of the frame header which is being send as if
// compression extension was negotiated.
func BitsSend(fseq int, rsv byte) (byte, error) {
	r1, r2, r3 := ws.RsvBits(rsv)
	if r1 {
		return rsv, fmt.Errorf("wsflate: compression bit is already set")
	}
	if fseq > 0 {
		// An endpoint MUST NOT set the "Per-Message Compressed"
		// bit of control frames and non-first fragments of a data
		// message. An endpoint receiving such a frame MUST _Fail
		// the WebSocket Connection_.
		return rsv, nil
	}
	return ws.Rsv(true, r2, r3), nil
}
