package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gobwas/httphead"
	"github.com/gobwas/pool/pbufio"
)

// Constants used by ConnUpgrader.
const (
	DefaultServerReadBufferSize  = 4096
	DefaultServerWriteBufferSize = 512
)

// Errors used by both client and server when preparing WebSocket handshake.
var (
	ErrHandshakeBadProtocol   = fmt.Errorf("handshake error: bad HTTP protocol version")
	ErrHandshakeBadMethod     = fmt.Errorf("handshake error: bad HTTP request method")
	ErrHandshakeBadHost       = fmt.Errorf("handshake error: bad %q header", headerHost)
	ErrHandshakeBadUpgrade    = fmt.Errorf("handshake error: bad %q header", headerUpgrade)
	ErrHandshakeBadConnection = fmt.Errorf("handshake error: bad %q header", headerConnection)
	ErrHandshakeBadSecAccept  = fmt.Errorf("handshake error: bad %q header", headerSecAccept)
	ErrHandshakeBadSecKey     = fmt.Errorf("handshake error: bad %q header", headerSecKey)
	ErrHandshakeBadSecVersion = fmt.Errorf("handshake error: bad %q header", headerSecVersion)
)

// ErrNotHijacker is an error returned when http.ResponseWriter does not
// implement http.Hijacker interface.
var ErrNotHijacker = fmt.Errorf("given http.ResponseWriter is not a http.Hijacker")

// DefaultHTTPUpgrader is an HTTPUpgrader that holds no options and is used by
// UpgradeHTTP function.
var DefaultHTTPUpgrader HTTPUpgrader

// UpgradeHTTP is like HTTPUpgrader{}.Upgrade().
func UpgradeHTTP(r *http.Request, w http.ResponseWriter, h http.Header) (conn net.Conn, rw *bufio.ReadWriter, hs Handshake, err error) {
	return DefaultHTTPUpgrader.Upgrade(r, w, h)
}

// DefaultUpgrader is an Upgrader that holds no options and is used by Upgrade
// function.
var DefaultUpgrader Upgrader

// Upgrade is like Upgrader{}.Upgrade().
func Upgrade(conn io.ReadWriter) (Handshake, error) {
	return DefaultUpgrader.Upgrade(conn)
}

// HTTPUpgrader contains options for upgrading connection to websocket from
// net/http Handler arguments.
type HTTPUpgrader struct {
	// Timeout is the maximum amount of time an Upgrade() will spent while
	// writing handshake response.
	//
	// The default is no timeout.
	Timeout time.Duration

	// Protocol is the select function that is used to select subprotocol from
	// list requested by client. If this field is set, then the first matched
	// protocol is sent to a client as negotiated.
	Protocol func(string) bool

	// Extension is the select function that is used to select extensions from
	// list requested by client. If this field is set, then the all matched
	// extensions are sent to a client as negotiated.
	Extension func(httphead.Option) bool
}

// Upgrade upgrades http connection to the websocket connection.
// Set of additional headers could be passed to be sent with the response after successful upgrade.
//
// It hijacks net.Conn from w and returns recevied net.Conn and bufio.ReadWriter.
// On successful handshake it returns Handshake struct describing handshake info.
func (u HTTPUpgrader) Upgrade(r *http.Request, w http.ResponseWriter, h http.Header) (conn net.Conn, rw *bufio.ReadWriter, hs Handshake, err error) {
	// See https://tools.ietf.org/html/rfc6455#section-4.1
	// The method of the request MUST be GET, and the HTTP version MUST be at least 1.1.
	if r.Method != http.MethodGet {
		err = ErrHandshakeBadMethod
		httpError(w, err.Error(), http.StatusMethodNotAllowed)
		return
	}
	if r.ProtoMajor < 1 || (r.ProtoMajor == 1 && r.ProtoMinor < 1) {
		err = ErrHandshakeBadProtocol
		httpError(w, err.Error(), http.StatusHTTPVersionNotSupported)
		return
	}
	if r.Host == "" {
		err = ErrHandshakeBadHost
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if u := httpGetHeader(r.Header, headerUpgrade); u != "websocket" && !strEqualFold(u, "websocket") {
		err = ErrHandshakeBadUpgrade
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if c := httpGetHeader(r.Header, headerConnection); c != "Upgrade" && !strHasToken(c, "upgrade") {
		err = ErrHandshakeBadConnection
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	nonce := httpGetHeader(r.Header, headerSecKey)
	if len(nonce) != nonceSize {
		err = ErrHandshakeBadSecKey
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if v := httpGetHeader(r.Header, headerSecVersion); v != "13" {
		err = ErrHandshakeBadSecVersion
		// According to RFC6455:
		//
		// If this version does not match a version understood by the server,
		// the server MUST abort the WebSocket handshake described in this
		// section and instead send an appropriate HTTP error code (such as 426
		// Upgrade Required) and a |Sec-WebSocket-Version| header field
		// indicating the version(s) the server is capable of understanding.
		//
		// So we branching here cause empty or not present version does not
		// meet the ABNF rules of RFC6455:
		//
		// version = DIGIT | (NZDIGIT DIGIT) |
		// ("1" DIGIT DIGIT) | ("2" DIGIT DIGIT)
		// ; Limited to 0-255 range, with no leading zeros
		//
		// That is, if version is really invalid – we sent 426 status, if it
		// not present or empty – it is 400.
		if v != "" {
			w.Header().Set(headerSecVersion, "13")
			httpError(w, err.Error(), http.StatusUpgradeRequired)
		} else {
			httpError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	if check := u.Protocol; check != nil {
		for _, v := range r.Header[headerSecProtocol] {
			var ok bool
			hs.Protocol, ok = strSelectProtocol(v, check)
			if !ok {
				err = ErrMalformedRequest
				httpError(w, err.Error(), http.StatusBadRequest)
				return
			}
			if hs.Protocol != "" {
				break
			}
		}
	}
	if check := u.Extension; check != nil {
		for _, v := range r.Header[headerSecExtensions] {
			var ok bool
			hs.Extensions, ok = strSelectExtensions(v, hs.Extensions, check)
			if !ok {
				err = ErrMalformedRequest
				httpError(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		err = ErrNotHijacker
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	conn, rw, err = hj.Hijack()
	if err != nil {
		httpError(w, "", http.StatusInternalServerError)
		return
	}

	// Clear deadlines set by server.
	conn.SetDeadline(noDeadline)
	if t := u.Timeout; t != 0 {
		conn.SetWriteDeadline(time.Now().Add(t))
		defer conn.SetWriteDeadline(noDeadline)
	}

	var hw func(io.Writer)
	if h != nil {
		hw = HeaderWriter(h)
	}

	httpWriteResponseUpgrade(rw.Writer, strToBytes(nonce), hs, hw)
	err = rw.Writer.Flush()

	return
}

// Upgrader contains options for upgrading connection to websocket.
type Upgrader struct {
	// ReadBufferSize and WriteBufferSize is an I/O buffer sizes.
	// They used to read and write http data while upgrading to WebSocket.
	// Allocated buffers are pooled with sync.Pool to avoid extra allocations.
	//
	// If a size is zero then default value is used.
	//
	// Usually it is useful to set read buffer size bigger than write buffer
	// size because incoming request could contain long header values, such as
	// Cookie. Response, in other way, could be big only if user write multiple
	// custom headers. Usually response takes less than 256 bytes.
	ReadBufferSize, WriteBufferSize int

	// Protocol is a select function that is used to select subprotocol
	// from list requested by client. If this field is set, then the first matched
	// protocol is sent to a client as negotiated.
	//
	// The argument is only valid until the callback returns.
	Protocol func([]byte) bool

	// ProtocolCustrom allow user to parse Sec-WebSocket-Protocol header manually.
	// Note that returned bytes must be valid until Upgrade returns.
	// If ProtocolCustom is set, it used instead of Protocol function.
	ProtocolCustom func([]byte) (string, bool)

	// Extension is a select function that is used to select extensions
	// from list requested by client. If this field is set, then the all matched
	// extensions are sent to a client as negotiated.
	//
	// The argument is only valid until the callback returns.
	//
	// According to the RFC6455 order of extensions passed by a client is
	// significant. That is, returning true from this function means that no
	// other extension with the same name should be checked because server
	// accepted the most preferable extension right now:
	// "Note that the order of extensions is significant.  Any interactions between
	// multiple extensions MAY be defined in the documents defining the extensions.
	// In the absence of such definitions, the interpretation is that the header
	// fields listed by the client in its request represent a preference of the
	// header fields it wishes to use, with the first options listed being most
	// preferable."
	Extension func(httphead.Option) bool

	// ExtensionCustorm allow user to parse Sec-WebSocket-Extensions header manually.
	// Note that returned options should be valid until Upgrade returns.
	// If ExtensionCustom is set, it used instead of Extension function.
	ExtensionCustom func([]byte, []httphead.Option) ([]httphead.Option, bool)

	// Header is a callback that will be called with io.Writer.
	// Write() calls that writer will put data in the response http headers
	// section.
	//
	// It used instead of http.Header mapping to avoid allocations in user land.
	//
	// Not that if present, this callback will be called for any result of
	// upgrading.
	Header func(io.Writer)

	// OnRequest is a callback that will be called after request line
	// successful parsing.
	//
	// The arguments are only valid until the callback returns.
	//
	// Returned value could be used to prevent processing request and response
	// with appropriate http status.
	OnRequest func(uri []byte) (err error, code int)

	// OnHost is a callback that will be called after "Host" header successful
	// parsing.
	//
	// The arguments are only valid until the callback returns.
	//
	// Returned value could be used to prevent processing request and response
	// with appropriate http status.
	//
	// It is separated from OnHeader callback because the Host header must be
	// present in each request since HTTP/1.1. Thus Host header is non-optional
	// and required for every WebSocket handshake.
	OnHost func(host []byte) (err error, code int)

	// OnHeader is a callback that will be called after successful parsing of
	// header, that is not used during WebSocket handshake procedure. That is,
	// it will be called with non-websocket headers, which could be relevant
	// for application-level logic.
	//
	// The arguments are only valid until the callback returns.
	//
	// Returned value could be used to prevent processing request and response
	// with appropriate http status.
	OnHeader func(key, value []byte) (err error, code int)

	// OnBeforeUpgrade is a callback that will be called before sending
	// successful upgrade response.
	//
	// Setting OnBeforeUpgrade allows user to make final application-level
	// checks and decide whether this connection is allowed to successfully
	// upgrade to WebSocket. That is, the session checks and other application
	// logic could be contained inside this callback.
	//
	// OnBeforeUpgrade could return header writer callback, that will be called
	// to provide some user land http headers in response.
	//
	// If by some reason connection should not be upgraded then OnBeforeUpgrade
	// should return error and appropriate http status code.
	//
	// Note that header writer callback will be called even if err is non-nil.
	OnBeforeUpgrade func() (header func(io.Writer), err error, code int)

	// TODO(gobwas): OnBeforeReject

	// TODO(gobwas): maybe use here io.WriterTo or something similar instead of
	// error missing header callback?
}

// Upgrade zero-copy upgrades connection to WebSocket. It interprets given conn
// as connection with incoming HTTP Upgrade request.
//
// It is a caller responsibility to manage i/o timeouts on conn.
//
// Non-nil error means that request for the WebSocket upgrade is invalid or
// malformed and usually connection should be closed.
// Even when error is non-nil Upgrade will write appropriate response into
// connection in compliance with RFC.
func (u Upgrader) Upgrade(conn io.ReadWriter) (hs Handshake, err error) {
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

	// Prepare I/O buffers.
	// TODO: make it configurable.
	br := pbufio.GetReader(conn,
		nonZero(u.ReadBufferSize, DefaultServerReadBufferSize),
	)
	bw := pbufio.GetWriter(conn,
		nonZero(u.WriteBufferSize, DefaultServerWriteBufferSize),
	)
	defer func() {
		pbufio.PutReader(br)
		pbufio.PutWriter(bw)
	}()

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

	// Use default http status code for errors.
	code := http.StatusBadRequest

	// Parse and check HTTP request.
	// As RFC6455 says:
	//   The client's opening handshake consists of the following parts. If the
	//   server, while reading the handshake, finds that the client did not
	//   send a handshake that matches the description below (note that as per
	//   [RFC2616], the order of the header fields is not important), including
	//   but not limited to any violations of the ABNF grammar specified for
	//   the components of the handshake, the server MUST stop processing the
	//   client's handshake and return an HTTP response with an appropriate
	//   error code (such as 400 Bad Request).
	//
	// See https://tools.ietf.org/html/rfc6455#section-4.2.1

	// An HTTP/1.1 or higher GET request, including a "Request-URI".
	//
	// Even if RFC says "1.1 or higher" without mentioning the part of the
	// version, we apply it only to minor part.
	if req.major != 1 || req.minor < 1 {
		// Abort processing the whole request because we do not even know how
		// to actually parse it.
		err = ErrHandshakeBadProtocol
		httpWriteResponseError(bw, err, http.StatusHTTPVersionNotSupported, nil)
		bw.Flush()
		return
	}
	if btsToString(req.method) != http.MethodGet {
		err = ErrHandshakeBadMethod
		httpWriteResponseError(bw, err, http.StatusMethodNotAllowed, nil)
		bw.Flush()
		return
	}
	if onRequest := u.OnRequest; onRequest != nil {
		if e, c := onRequest(req.uri); e != nil {
			err = e
			code = c
		}
	}
	// Start headers read/parse loop.
	var (
		// headerSeen reports which header was seen by setting corresponding
		// bit on.
		headerSeen byte
		nonce      nonce
		hcb        func(io.Writer)
		hw         headerWriter
	)
	if u.Header != nil {
		hw.add(u.Header)
	}
	for err == nil {
		line, e := readLine(br)
		if e != nil {
			err = e
			return
		}
		if len(line) == 0 {
			// Blank line, no more lines to read.
			break
		}

		k, v, ok := httpParseHeaderLine(line)
		if !ok {
			err = ErrMalformedRequest
			httpWriteResponseError(bw, err, http.StatusBadRequest, nil)
			bw.Flush()
			return
		}

		switch btsToString(k) {
		case headerHost:
			headerSeen |= headerSeenHost
			if onHost := u.OnHost; onHost != nil {
				if e, c := onHost(v); e != nil {
					err = e
					code = c
				}
			}

		case headerUpgrade:
			headerSeen |= headerSeenUpgrade
			if !bytes.Equal(v, specHeaderValueUpgrade) && !btsEqualFold(v, specHeaderValueUpgrade) {
				err = ErrHandshakeBadUpgrade
			}

		case headerConnection:
			headerSeen |= headerSeenConnection
			if !bytes.Equal(v, specHeaderValueConnection) && !btsHasToken(v, specHeaderValueConnectionLower) {
				err = ErrHandshakeBadConnection
			}

		case headerSecKey:
			headerSeen |= headerSeenSecKey
			if len(v) != nonceSize {
				err = ErrHandshakeBadSecKey
			} else {
				copy(nonce[:], v)
			}

		case headerSecVersion:
			headerSeen |= headerSeenSecVersion
			if !bytes.Equal(v, specHeaderValueSecVersion) {
				// According to RFC6455:
				//
				// If this version does not match a version understood by the
				// server, the server MUST abort the WebSocket handshake
				// described in this section and instead send an appropriate
				// HTTP error code (such as 426 Upgrade Required) and a
				// |Sec-WebSocket-Version| header field indicating the
				// version(s) the server is capable of understanding.
				hw.add(headerWriterSecVersion)
				err = ErrHandshakeBadSecVersion
				code = http.StatusUpgradeRequired
			}

		case headerSecProtocol:
			if custom, check := u.ProtocolCustom, u.Protocol; hs.Protocol == "" && (custom != nil || check != nil) {
				var ok bool
				if custom != nil {
					hs.Protocol, ok = custom(v)
				} else {
					hs.Protocol, ok = btsSelectProtocol(v, check)
				}
				if !ok {
					err = ErrMalformedRequest
				}
			}

		case headerSecExtensions:
			if custom, check := u.ExtensionCustom, u.Extension; custom != nil || check != nil {
				var ok bool
				if custom != nil {
					hs.Extensions, ok = custom(v, hs.Extensions)
				} else {
					hs.Extensions, ok = btsSelectExtensions(v, hs.Extensions, check)
				}
				if !ok {
					err = ErrMalformedRequest
				}
			}

		default:
			if onHeader := u.OnHeader; onHeader != nil {
				if e, c := onHeader(k, v); e != nil {
					err = e
					code = c
				}
			}
		}
	}
	switch {
	case err == nil && headerSeen != headerSeenAll:
		switch {
		case headerSeen&headerSeenHost == 0:
			// As RFC2616 says:
			//   A client MUST include a Host header field in all HTTP/1.1
			//   request messages. If the requested URI does not include an
			//   Internet host name for the service being requested, then the
			//   Host header field MUST be given with an empty value. An
			//   HTTP/1.1 proxy MUST ensure that any request message it
			//   forwards does contain an appropriate Host header field that
			//   identifies the service being requested by the proxy. All
			//   Internet-based HTTP/1.1 servers MUST respond with a 400 (Bad
			//   Request) status code to any HTTP/1.1 request message which
			//   lacks a Host header field.
			err = ErrHandshakeBadHost
		case headerSeen&headerSeenUpgrade == 0:
			err = ErrHandshakeBadUpgrade
		case headerSeen&headerSeenConnection == 0:
			err = ErrHandshakeBadConnection
		case headerSeen&headerSeenSecVersion == 0:
			// In cause of empty or not present version we do not send 426 status,
			// because it does not meet the ABNF rules of RFC6455:
			//
			// version = DIGIT | (NZDIGIT DIGIT) |
			// ("1" DIGIT DIGIT) | ("2" DIGIT DIGIT)
			// ; Limited to 0-255 range, with no leading zeros
			//
			// That is, if version is really invalid – we sent 426 status as above, if it
			// not present – it is 400.
			err = ErrHandshakeBadSecVersion
		case headerSeen&headerSeenSecKey == 0:
			err = ErrHandshakeBadSecKey
		default:
			panic("unknown headers state")
		}

	case err == nil && u.OnBeforeUpgrade != nil:
		hcb, err, code = u.OnBeforeUpgrade()
		if hcb != nil {
			hw.add(hcb)
		}
	}
	if err != nil {
		httpWriteResponseError(bw, err, code, hw.flush)
		// Do not store Flush() error to not override already existing one.
		bw.Flush()
		return
	}

	httpWriteResponseUpgrade(bw, nonce.bytes(), hs, hw.flush)
	err = bw.Flush()

	return
}

type headerWriter struct {
	cb [3]func(io.Writer)
	n  int
}

func (w *headerWriter) add(cb func(io.Writer)) {
	if w.n == len(w.cb) {
		panic("header callbacks overflow")
	}
	w.cb[w.n] = cb
	w.n++
}

func (w headerWriter) flush(to io.Writer) {
	for i := 0; i < w.n; i++ {
		w.cb[i](to)
	}
}

func headerWriterSecVersion(w io.Writer) {
	w.Write(btsErrorVersion)
}
