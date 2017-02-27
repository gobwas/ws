package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	_ "unsafe" // for go:linkname

	"github.com/gobwas/pool/pbufio"
)

var ErrNotHijacker = fmt.Errorf("given http.ResponseWriter is not a http.Hijacker")

// DefaultUpgrader is upgrader that holds no options and is used by Upgrade function.
var DefaultUpgrader Upgrader

// Upgrade is like Upgrader{}.Upgrade.
func Upgrade(r *http.Request, w http.ResponseWriter, h http.Header) (conn net.Conn, rw *bufio.ReadWriter, hs Handshake, err error) {
	return DefaultUpgrader.Upgrade(r, w, h)
}

// Upgrader contains options for upgrading http connection to websocket.
type Upgrader struct {
	// Protocol is the select function that is used to select subprotocol
	// from list requested by client. If this field is set, then the first matched
	// protocol is sent to a client as negotiated.
	Protocol func(string) bool

	// Extension is the select function that is used to select extensions
	// from list requested by client. If this field is set, then the all matched
	// extensions are sent to a client as negotiated.
	Extension func(string) bool
}

// Upgrade upgrades http connection to the websocket connection.
// Set of additional headers could be passed to be sent with the response after successful upgrade.
//
// It hijacks net.Conn from w and returns recevied net.Conn and bufio.ReadWriter.
// On successful handshake it returns Handshake struct describing handshake info.
func (u Upgrader) Upgrade(r *http.Request, w http.ResponseWriter, h http.Header) (conn net.Conn, rw *bufio.ReadWriter, hs Handshake, err error) {
	// See https://tools.ietf.org/html/rfc6455#section-4.1
	// The method of the request MUST be GET, and the HTTP version MUST be at least 1.1.
	if r.Method != http.MethodGet {
		err = ErrBadHttpRequestMethod
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.ProtoMajor < 1 || (r.ProtoMajor == 1 && r.ProtoMinor < 1) {
		err = ErrBadHttpRequestProto
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Host == "" {
		err = ErrBadHost
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if u := httpGetHeader(r.Header, headerUpgrade); u != "websocket" && !strEqualFold(u, "websocket") {
		err = ErrBadUpgrade
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if c := httpGetHeader(r.Header, headerConnection); c != "Upgrade" && !strHasToken(c, "upgrade") {
		err = ErrBadConnection
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nonce := httpGetHeader(r.Header, headerSecKey)
	if len(nonce) != nonceSize {
		err = ErrBadSecKey
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if v := httpGetHeader(r.Header, headerSecVersion); v != "13" {
		err = ErrBadSecVersion
		w.Header().Set(headerSecVersion, "13")
		http.Error(w, err.Error(), http.StatusUpgradeRequired)
		return
	}

	if check := u.Protocol; check != nil {
		for _, v := range r.Header[headerSecProtocol] {
			var ok, done bool
			hs.Protocol, done, ok = strSelectProtocol(v, check)
			if !ok {
				err = ErrMalformedHttpRequest
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if done {
				break
			}
		}
	}
	if check := u.Extension; check != nil {
		// TODO(gobwas) parse extensions.
		//	hs.Extensions = selectExtensions(e, c.Extension)
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		err = ErrNotHijacker
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	conn, rw, err = hj.Hijack()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	httpWriteUpgrade(rw.Writer, strToNonce(nonce), hs, h)
	err = rw.Writer.Flush()

	return
}

type ConnUpgrader struct {
	// Protocol is the select function that is used to select subprotocol
	// from list requested by client. If this field is set, then the first matched
	// protocol is sent to a client as negotiated.
	//
	// The argument is only valid until the callback returns.
	Protocol func([]byte) bool

	// Extension is the select function that is used to select extensions
	// from list requested by client. If this field is set, then the all matched
	// extensions are sent to a client as negotiated.
	//
	// The argument is only valid until the callback returns.
	Extension func([]byte) bool

	// OnRequest is a callback that will be called after request line and
	// "Host" header successful parsing. Setting this field helps to implement
	// some application logic.
	//
	// The arguments are only valid until the callback returns.
	//
	// Returned value could be used to prevent processing request and response
	// with appropriate http status.
	OnRequest func(host, uri []byte) (err error, code int, header HttpHeaderWriter)

	// OnHeader is a callback that will be called after successful parsing of
	// header, that is not used during WebSocket handshake procedure. That is,
	// it will be called with non-websocket headers, which could be relevant
	// for application-level logic.
	//
	// The arguments are only valid until the callback returns.
	//
	// Returned value could be used to prevent processing request and response
	// with appropriate http status.
	OnHeader func(key, value []byte) (err error, code int, header HttpHeaderWriter)
}

var (
	expHeaderUpgrade         = []byte("websocket")
	expHeaderConnection      = []byte("Upgrade")
	expHeaderConnectionLower = []byte("upgrade")
	expHeaderSecVersion      = []byte("13")
)

func (u ConnUpgrader) Upgrade(conn io.ReadWriter, h http.Header) (hs Handshake, err error) {
	// headerSeen constants helps to report whether or not some header was seen
	// during reading request bytes.
	const (
		headerSeenHost = 1 << iota
		headerSeenUpgrade
		headerSeenConnection
		headerSeenSecVersion
		headerSeenSecKey

		// headerSeenAll is the value that we expect to receive at the end of
		// headers read/parse loop.
		headerSeenAll = 0 |
			headerSeenHost |
			headerSeenUpgrade |
			headerSeenConnection |
			headerSeenSecVersion |
			headerSeenSecKey
	)

	br := pbufio.GetReader(conn, 512)
	defer pbufio.PutReader(br, 512)

	// Read HTTP request line like "GET /ws HTTP/1.1".
	rl, err := readLine(br)
	if err != nil {
		return
	}
	// Parse request line data like HTTP version, uri and method.
	req, err := httpParseRequestLine(rl)
	if err != nil {
		return
	}

	var (
		// Use BadRequest as default error status code.
		code = http.StatusBadRequest
		errh HttpHeaderWriter
	)

	bw := pbufio.GetWriter(conn, 512)
	defer pbufio.PutWriter(bw)

	// See https://tools.ietf.org/html/rfc6455#section-4.1
	// The method of the request MUST be GET, and the HTTP version MUST be at least 1.1.
	if btsToString(req.method) != http.MethodGet {
		err = ErrBadHttpRequestMethod
		httpWriteResponseError(bw, err, code, nil)
		bw.Flush()
		return
	}
	if req.major < 1 || (req.major == 1 && req.minor < 1) {
		err = ErrBadHttpRequestProto
		httpWriteResponseError(bw, err, code, nil)
		bw.Flush()
		return
	}

	// Start headers read/parse loop.
	var (
		// headerSeen reports which header was seen by setting corresponding
		// bit on.
		headerSeen byte
		nonce      []byte
	)
	for {
		line, e := readLine(br)
		if e != nil {
			err = e
			return
		}

		// Blank line, no more lines to read.
		if len(line) == 0 {
			break
		}

		k, v, ok := httpParseHeaderLine(line)
		if !ok {
			err = ErrMalformedHttpRequest
			return
		}

		switch btsToString(k) {
		case headerHost:
			headerSeen |= headerSeenHost
			if len(v) == 0 {
				if err == nil {
					err = ErrBadHost
				}
			} else if onRequest := u.OnRequest; err == nil && onRequest != nil {
				if e, c, hw := onRequest(v, req.uri); e != nil {
					err = e
					code = c
					errh = hw
				}
			}

		case headerUpgrade:
			headerSeen |= headerSeenUpgrade
			if err == nil && !bytes.Equal(v, expHeaderUpgrade) && !btsEqualFold(v, expHeaderUpgrade) {
				err = ErrBadUpgrade
			}
		case headerConnection:
			headerSeen |= headerSeenConnection
			if err == nil && !bytes.Equal(v, expHeaderConnection) && !btsHasToken(v, expHeaderConnectionLower) {
				err = ErrBadConnection
			}
		case headerSecKey:
			headerSeen |= headerSeenSecKey
			if err == nil && len(v) != nonceSize {
				err = ErrBadSecKey
			}
			nonce = v
		case headerSecVersion:
			headerSeen |= headerSeenSecVersion
			if err == nil && !bytes.Equal(v, expHeaderSecVersion) {
				err = ErrBadSecVersion
				code = http.StatusUpgradeRequired
				errh = headerWriterSecVersion
			}

		case headerSecProtocol:
			if check := u.Protocol; err == nil && check != nil && hs.Protocol == "" {
				p, selected, ok := btsSelectProtocol(v, check)
				if !ok {
					err = ErrMalformedHttpRequest
				}
				if selected {
					hs.Protocol = string(p)
				}
			}
		case headerSecExtensions:
			// TODO(gobwas) select extensions.

		default:
			if onHeader := u.OnHeader; err == nil && onHeader != nil {
				if e, c, hw := onHeader(k, v); e != nil {
					err = e
					code = c
					errh = hw
				}
			}
		}
	}

	if err == nil && headerSeen != headerSeenAll {
		switch {
		case headerSeen & ^byte(headerSeenHost) == 0:
			err = ErrBadHost
		case headerSeen & ^byte(headerSeenUpgrade) == 0:
			err = ErrBadUpgrade
		case headerSeen & ^byte(headerSeenConnection) == 0:
			err = ErrBadConnection
		case headerSeen & ^byte(headerSeenSecVersion) == 0:
			err = ErrBadSecVersion
		case headerSeen & ^byte(headerSeenSecKey) == 0:
			err = ErrBadSecKey
		default:
			panic("unknown headers state")
		}
	}

	if err != nil {
		httpWriteResponseError(bw, err, code, errh)
		bw.Flush()
		return
	}

	httpWriteUpgrade(bw, btsToNonce(nonce), hs, h)
	err = bw.Flush()

	return
}

func headerWriterSecVersion(bw *bufio.Writer) {
	httpWriteHeader(bw, headerSecVersion, "13")
}

func selectExtensions(h []string, ok func(string) bool) []string {
	// TODO(gobwas): parse extensions with params
	return nil
}
