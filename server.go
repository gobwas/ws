package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	_ "unsafe" // for go:linkname

	"github.com/gobwas/pool/pbufio"
)

// Errors used by upgraders.
var (
	ErrMalformedHttpRequest = fmt.Errorf("malformed HTTP request")

	ErrBadHttpRequestProto  = fmt.Errorf("bad HTTP request protocol version")
	ErrBadHttpRequestMethod = fmt.Errorf("bad HTTP request method")

	ErrBadHost       = fmt.Errorf("bad %q header", headerHost)
	ErrBadUpgrade    = fmt.Errorf("bad %q header", headerUpgrade)
	ErrBadConnection = fmt.Errorf("bad %q header", headerConnection)
	ErrBadSecAccept  = fmt.Errorf("bad %q header", headerSecAccept)
	ErrBadSecKey     = fmt.Errorf("bad %q header", headerSecKey)
	ErrBadSecVersion = fmt.Errorf("bad %q header", headerSecVersion)
	ErrBadHijacker   = fmt.Errorf("given http.ResponseWriter is not a http.Hijacker")
)

// SelectFromSlice creates accept function that could be used as Protocol/Extension
// select during upgrade.
func SelectFromSlice(accept []string) func(string) bool {
	if len(accept) > 16 {
		mp := make(map[string]struct{}, len(accept))
		for _, p := range accept {
			mp[p] = struct{}{}
		}
		return func(p string) bool {
			_, ok := mp[p]
			return ok
		}
	}
	return func(p string) bool {
		for _, ok := range accept {
			if p == ok {
				return true
			}
		}
		return false
	}
}

// SelectEqual creates accept function that could be used as Protocol/Extension
// select during upgrade.
func SelectEqual(v string) func(string) bool {
	return func(p string) bool {
		return v == p
	}
}

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
	if u := getHeader(r.Header, headerUpgrade); u != "websocket" && !strEqualFold(u, "websocket") {
		err = ErrBadUpgrade
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if c := getHeader(r.Header, headerConnection); c != "Upgrade" && !strHasToken(c, "upgrade") {
		err = ErrBadConnection
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nonce := getHeader(r.Header, headerSecKey)
	if len(nonce) != nonceSize {
		err = ErrBadSecKey
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if v := getHeader(r.Header, headerSecVersion); v != "13" {
		err = ErrBadSecVersion
		w.Header().Set(headerSecVersion, "13")
		http.Error(w, err.Error(), http.StatusUpgradeRequired)
		return
	}

	var check func(string) bool
	if check = u.Protocol; check != nil {
		for _, v := range r.Header[headerSecProtocol] {
			if check(v) {
				hs.Protocol = v
				break
			}
		}
	}
	if check = u.Extension; check != nil {
		// TODO(gobwas) parse extensions.
		//	hs.Extensions = selectExtensions(e, c.Extension)
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		err = ErrBadHijacker
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	conn, rw, err = hj.Hijack()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeUpgrade(rw.Writer, strToNonce(nonce), hs, h)
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
	OnRequest func(host, uri []byte) (err error, code int, header HeaderWriter)

	// OnHeader is a callback that will be called after successful parsing of
	// header, that is not used during WebSocket handshake procedure. That is,
	// it will be called with non-websocket headers, which could be relevant
	// for application-level logic.
	//
	// The arguments are only valid until the callback returns.
	//
	// Returned value could be used to prevent processing request and response
	// with appropriate http status.
	OnHeader func(key, value []byte) (err error, code int, header HeaderWriter)
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
		errh HeaderWriter
	)

	bw := pbufio.GetWriter(conn, 512)
	defer pbufio.PutWriter(bw)

	// See https://tools.ietf.org/html/rfc6455#section-4.1
	// The method of the request MUST be GET, and the HTTP version MUST be at least 1.1.
	if btsToString(req.method) != http.MethodGet {
		err = ErrBadHttpRequestMethod
		writeResponseError(bw, err, code, nil)
		bw.Flush()
		return
	}
	if req.major < 1 || (req.major == 1 && req.minor < 1) {
		err = ErrBadHttpRequestProto
		writeResponseError(bw, err, code, nil)
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
			if check := u.Protocol; check != nil && hs.Protocol == "" {
				if check(v) {
					hs.Protocol = string(v)
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
		writeResponseError(bw, err, code, errh)
		bw.Flush()
		return
	}

	writeUpgrade(bw, btsToNonce(nonce), hs, h)
	err = bw.Flush()

	return
}

// getHeader is the same as textproto.MIMEHeader.Get, except the thing,
// that key is already canonical. This helps to increase performance.
func getHeader(h http.Header, key string) string {
	if h == nil {
		return ""
	}
	v := h[key]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func writeHeader(bw *bufio.Writer, key, value string) {
	writeHeaderKey(bw, key)
	writeHeaderValue(bw, value)
}

func writeHeaderKey(bw *bufio.Writer, key string) {
	bw.WriteString(key)
	bw.WriteString(colonAndSpace)
}

func writeHeaderValue(bw *bufio.Writer, value string) {
	bw.WriteString(value)
	bw.WriteString(crlf)
}

func writeHeaderValueBytes(bw *bufio.Writer, value []byte) {
	bw.Write(value)
	bw.WriteString(crlf)
}

// HeaderWriter represents low level HTTP header writer.
// If you want to dump some http.Header instance into given bufio.Writer, you
// could do something like this:
//
// h := make(http.Header{"foo": []string{"bar"}})
// return func(bw *bufio.Writer) { h.Write(bw) }
//
// This type is for clients ability to avoid allocations and call bufio.Writer
// methods directly.
type HeaderWriter func(*bufio.Writer)

func headerWriterSecVersion(bw *bufio.Writer) {
	writeHeader(bw, headerSecVersion, "13")
}

func writeUpgrade(bw *bufio.Writer, nonce [nonceSize]byte, hs Handshake, h http.Header) {
	bw.WriteString(textUpgrade)

	writeHeaderKey(bw, headerSecAccept)
	writeAccept(bw, nonce)
	bw.WriteString(crlf)

	if hs.Protocol != "" {
		writeHeader(bw, headerSecProtocol, hs.Protocol)
	}
	if len(hs.Extensions) > 0 {
		// TODO(gobwas)
		//	if len(hs.Extensions) > 0 {
		//		writeHeader(bw, headerSecExtensions, strings.Join(hs.Extensions, ", "))
		//	}
	}
	for key, values := range h {
		for _, val := range values {
			writeHeader(bw, key, val)
		}
	}

	bw.WriteString(crlf)
}

func writeResponseError(bw *bufio.Writer, err error, code int, hw HeaderWriter) {
	switch code {
	case http.StatusBadRequest:
		bw.WriteString(textBadRequest)
	default:
		bw.WriteString("HTTP/1.1 ")
		bw.WriteString(strconv.FormatInt(int64(code), 10))
		bw.WriteByte(' ')
		bw.WriteString(http.StatusText(code))
		bw.WriteString(crlf)
		bw.WriteString(textErrorContent)
	}
	if hw != nil {
		hw(bw)
	}
	bw.WriteString(crlf)
	if err != nil {
		bw.WriteString(err.Error())
		bw.WriteByte('\n') // Just to be consistent with http.Error().
	}
}

func selectProtocol(h string, ok func(string) bool) string {
	var start int
	for i := 0; i < len(h); i++ {
		c := h[i]
		// The elements that comprise this value MUST be non-empty strings with characters in the range
		// U+0021 to U+007E not including separator characters as defined in [RFC2616]
		// and MUST all be unique strings.
		if c != ',' && '!' <= c && c <= '~' {
			continue
		}
		if str := h[start:i]; len(str) > 0 && ok(str) {
			return str
		}
		start = i + 1
	}
	if str := h[start:]; len(str) > 0 && ok(str) {
		return str
	}
	return ""
}

func selectExtensions(h []string, ok func(string) bool) []string {
	// TODO(gobwas): parse extensions with params
	return nil
}
