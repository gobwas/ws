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

const (
	textErrorContent = "Content-Type: text/plain; charset=utf-8\r\n"
	textUpgrade      = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
	textBadRequest   = "HTTP/1.1 400 Bad Request\r\n" + textErrorContent
	crlf             = "\r\n"
	colonAndSpace    = ": "
)

// Errors used by upgraders.
var (
	ErrMalformedHttpRequest = fmt.Errorf("malformed HTTP request")

	ErrBadHttpRequestVersion = fmt.Errorf("bad HTTP request version")
	ErrBadHttpRequestMethod  = fmt.Errorf("bad HTTP request method")

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

type Handshaker struct {
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

type ConnUpgrader struct {
	// Protocol is the select function that is used to select subprotocol
	// from list requested by client. If this field is set, then the first matched
	// protocol is sent to a client as negotiated.
	Protocol func(string) bool

	// Extension is the select function that is used to select extensions
	// from list requested by client. If this field is set, then the all matched
	// extensions are sent to a client as negotiated.
	Extension func(string) bool

	Route  func(host, uri []byte) (err error, code int)
	Header func(key, value []byte) (err error, code int)
}

func readLine(br *bufio.Reader) (line []byte, err error) {
	var more bool
	var bts []byte
	for {
		bts, more, err = br.ReadLine()
		if err != nil {
			return
		}
		// Avoid copying bytes to the nil slice.
		if line == nil {
			line = bts
		} else {
			line = append(line, bts...)
		}
		if !more {
			break
		}
	}
	return
}

var (
	httpVersion1_0    = []byte("HTTP/1.0")
	httpVersion1_1    = []byte("HTTP/1.1")
	httpVersionPrefix = []byte("HTTP/")
)

func parseHttpVersion(bts []byte) (major, minor int, ok bool) {
	switch {
	case bytes.Equal(bts, httpVersion1_0):
		return 1, 0, true
	case bytes.Equal(bts, httpVersion1_1):
		return 1, 1, true
	case len(bts) < 8:
		return
	case !bytes.Equal(bts[:5], httpVersionPrefix):
		return
	}

	bts = bts[5:]

	dot := bytes.IndexByte(bts, '.')
	if dot == -1 {
		return
	}
	var err error
	major, err = asciiToInt(bts[:dot])
	if err != nil {
		return
	}
	minor, err = asciiToInt(bts[dot+1:])
	if err != nil {
		return
	}

	return major, minor, true
}

const (
	headerSeenHost = 1 << iota
	headerSeenUpgrade
	headerSeenConnection
	headerSeenSecVersion
	headerSeenSecKey

	headerSeenAll = 0 |
		headerSeenHost |
		headerSeenUpgrade |
		headerSeenConnection |
		headerSeenSecVersion |
		headerSeenSecKey
)

var expHeaderUpgrade = []byte("websocket")
var expHeaderConnection = []byte("Upgrade")
var expHeaderConnectionLower = []byte("upgrade")
var expHeaderSecVersion = []byte("13")

func (u ConnUpgrader) Upgrade(conn io.ReadWriter, h http.Header) (hs Handshake, err error) {
	br := pbufio.GetReader(conn, 512)
	defer pbufio.PutReader(br, 512)

	//http.ReadRequest()
	req, err := parseRequestLine(br)
	if err != nil {
		return
	}

	// See https://tools.ietf.org/html/rfc6455#section-4.1
	// The method of the request MUST be GET, and the HTTP version MUST be at least 1.1.
	if btsToString(req.method) != "GET" {
		err = ErrBadHttpRequestMethod
	}
	if err == nil && (req.major != 1 || req.minor < 1) {
		err = ErrBadHttpRequestVersion
	}

	var (
		headerSeen byte
		nonce      []byte

		code = http.StatusBadRequest
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

		k, v, ok := parseHeaderLine(line)
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
			} else if onRoute := u.Route; onRoute != nil {
				if e, c := onRoute(v, req.uri); err == nil && e != nil {
					err = e
					code = c
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
		case headerSecVersion:
			headerSeen |= headerSeenSecVersion
			if err == nil && !bytes.Equal(v, expHeaderSecVersion) {
				err = ErrBadSecVersion
			}
		case headerSecKey:
			headerSeen |= headerSeenSecKey
			if err == nil && len(v) != nonceSize {
				err = ErrBadSecKey
			}
			nonce = v

		case headerSecProtocol:
			if check := u.Protocol; check != nil && hs.Protocol == "" {
				if check(btsToString(v)) {
					// TODO(gobwas) we could avoid copying here by
					// creating var [64]byte holder for subprotocol value,
					// and then dumping it below when saying client which protocol
					// we selected.
					hs.Protocol = string(v)
				}
			}
		case headerSecExtensions:
			// TODO(gobwas) select extensions.

		default:
			if onHeader := u.Header; onHeader != nil {
				if e, c := onHeader(k, v); err == nil && e != nil {
					err = e
					code = c
				}
			}
		}
	}

	if headerSeen != headerSeenAll {
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

	bw := pbufio.GetWriter(conn, 512)
	defer pbufio.PutWriter(bw)

	if err != nil {
		writeError(bw, err.Error(), code)
		return
	}

	err = writeUpgrade(bw, btsToNonce(nonce), hs, h)

	return
}

func parseHeaderLine(line []byte) (k, v []byte, ok bool) {
	colon := bytes.IndexByte(line, ':')
	if colon == -1 {
		return
	}

	k = btrim(line[:colon])
	canonicalizeHeaderKey(k)

	v = btrim(line[colon+1:])

	return k, v, true
}

type requestLine struct {
	method, uri  []byte
	major, minor int
}

func parseRequestLine(br *bufio.Reader) (req requestLine, err error) {
	line, err := readLine(br)
	if err != nil {
		return
	}

	var proto []byte
	req.method, req.uri, proto = bsplit3(line, ' ')

	var ok bool
	req.major, req.minor, ok = parseHttpVersion(proto)
	if !ok {
		err = ErrMalformedHttpRequest
		return
	}

	return
}

// Upgrade upgrades http connection to the websocket connection.
// Set of additional headers could be passed to be sent with the response after successful upgrade.
//
// It hijacks net.Conn from w and returns recevied net.Conn and bufio.ReadWriter.
// On successful handshake it returns Handshake struct describing handshake info.
func (u Upgrader) Upgrade(r *http.Request, w http.ResponseWriter, h http.Header) (conn net.Conn, rw *bufio.ReadWriter, hs Handshake, err error) {
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
	if v := getHeader(r.Header, headerSecVersion); v != "13" {
		err = ErrBadSecVersion
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	nonce := getHeader(r.Header, headerSecKey)
	if len(nonce) != nonceSize {
		err = ErrBadSecKey
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	err = writeUpgrade(rw.Writer, strToNonce(nonce), hs, h)
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

func writeUpgrade(bw *bufio.Writer, nonce [nonceSize]byte, hs Handshake, h http.Header) error {
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

	return bw.Flush()
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

func writeError(bw *bufio.Writer, err string, code int) {
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

	writeHeader(bw, "Content-Length", strconv.FormatInt(int64(len(err)), 10))
	bw.WriteString(crlf)
	bw.WriteString(err)
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
