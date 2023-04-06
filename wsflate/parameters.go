package wsflate

import (
	"fmt"
	"strconv"

	"github.com/gobwas/httphead"
)

const (
	ExtensionName = "permessage-deflate"

	serverNoContextTakeover = "server_no_context_takeover"
	clientNoContextTakeover = "client_no_context_takeover"
	serverMaxWindowBits     = "server_max_window_bits"
	clientMaxWindowBits     = "client_max_window_bits"
)

var (
	ExtensionNameBytes = []byte(ExtensionName)

	serverNoContextTakeoverBytes = []byte(serverNoContextTakeover)
	clientNoContextTakeoverBytes = []byte(clientNoContextTakeover)
	serverMaxWindowBitsBytes     = []byte(serverMaxWindowBits)
	clientMaxWindowBitsBytes     = []byte(clientMaxWindowBits)
)

var windowBits [8][]byte

func init() {
	for i := range windowBits {
		windowBits[i] = []byte(strconv.Itoa(i + 8))
	}
}

// Parameters contains compression extension options.
type Parameters struct {
	ServerNoContextTakeover bool
	ClientNoContextTakeover bool
	ServerMaxWindowBits     WindowBits
	ClientMaxWindowBits     WindowBits
}

// WindowBits specifies window size accordingly to RFC.
// Use its Bytes() method to obtain actual size of window in bytes.
type WindowBits byte

// Defined reports whether window bits were specified.
func (b WindowBits) Defined() bool {
	return b > 0
}

// Bytes returns window size in number of bytes.
func (b WindowBits) Bytes() int {
	return 1 << uint(b)
}

const (
	MaxLZ77WindowSize = 32768 // 2^15
)

// Parse reads parameters from given HTTP header option accordingly to RFC.
//
// It returns non-nil error at least in these cases:
//   - The negotiation offer contains an extension parameter not defined for
//     use in an offer/response.
//   - The negotiation offer/response contains an extension parameter with an
//     invalid value.
//   - The negotiation offer/response contains multiple extension parameters
//     with the same name.
func (p *Parameters) Parse(opt httphead.Option) (err error) {
	const (
		clientMaxWindowBitsSeen = 1 << iota
		serverMaxWindowBitsSeen
		clientNoContextTakeoverSeen
		serverNoContextTakeoverSeen
	)

	// Reset to not mix parsed data from previous Parse() calls.
	*p = Parameters{}

	var seen byte
	opt.Parameters.ForEach(func(key, val []byte) (ok bool) {
		switch string(key) {
		case clientMaxWindowBits:
			if len(val) == 0 {
				p.ClientMaxWindowBits = 1
				return true
			}
			if seen&clientMaxWindowBitsSeen != 0 {
				err = paramError("duplicate", key, val)
				return false
			}
			seen |= clientMaxWindowBitsSeen
			if p.ClientMaxWindowBits, ok = bitsFromASCII(val); !ok {
				err = paramError("invalid", key, val)
				return false
			}

		case serverMaxWindowBits:
			if len(val) == 0 {
				err = paramError("invalid", key, val)
				return false
			}
			if seen&serverMaxWindowBitsSeen != 0 {
				err = paramError("duplicate", key, val)
				return false
			}
			seen |= serverMaxWindowBitsSeen
			if p.ServerMaxWindowBits, ok = bitsFromASCII(val); !ok {
				err = paramError("invalid", key, val)
				return false
			}

		case clientNoContextTakeover:
			if len(val) > 0 {
				err = paramError("invalid", key, val)
				return false
			}
			if seen&clientNoContextTakeoverSeen != 0 {
				err = paramError("duplicate", key, val)
				return false
			}
			seen |= clientNoContextTakeoverSeen
			p.ClientNoContextTakeover = true

		case serverNoContextTakeover:
			if len(val) > 0 {
				err = paramError("invalid", key, val)
				return false
			}
			if seen&serverNoContextTakeoverSeen != 0 {
				err = paramError("duplicate", key, val)
				return false
			}
			seen |= serverNoContextTakeoverSeen
			p.ServerNoContextTakeover = true

		default:
			err = paramError("unexpected", key, val)
			return false
		}
		return true
	})
	return err
}

// Option encodes parameters into HTTP header option.
func (p Parameters) Option() httphead.Option {
	opt := httphead.Option{
		Name: ExtensionNameBytes,
	}
	setBool(&opt, serverNoContextTakeoverBytes, p.ServerNoContextTakeover)
	setBool(&opt, clientNoContextTakeoverBytes, p.ClientNoContextTakeover)
	setBits(&opt, serverMaxWindowBitsBytes, p.ServerMaxWindowBits)
	setBits(&opt, clientMaxWindowBitsBytes, p.ClientMaxWindowBits)
	return opt
}

func isValidBits(x int) bool {
	return 8 <= x && x <= 15
}

func bitsFromASCII(p []byte) (WindowBits, bool) {
	n, ok := httphead.IntFromASCII(p)
	if !ok || !isValidBits(n) {
		return 0, false
	}
	return WindowBits(n), true
}

func setBits(opt *httphead.Option, name []byte, bits WindowBits) {
	if bits == 0 {
		return
	}
	if bits == 1 {
		opt.Parameters.Set(name, nil)
		return
	}
	if !isValidBits(int(bits)) {
		panic(fmt.Sprintf("wsflate: invalid bits value: %d", bits))
	}
	opt.Parameters.Set(name, windowBits[bits-8])
}

func setBool(opt *httphead.Option, name []byte, flag bool) {
	if flag {
		opt.Parameters.Set(name, nil)
	}
}

func paramError(reason string, key, val []byte) error {
	return fmt.Errorf(
		"wsflate: %s extension parameter %q: %q",
		reason, key, val,
	)
}
