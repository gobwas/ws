package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strconv"

	"github.com/gobwas/httphead"
)

const (
	textErrorContent    = "Content-Type: text/plain; charset=utf-8\r\nX-Content-Type-Options: nosniff\r\n"
	textUpgrade         = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
	textBadRequest      = "HTTP/1.1 400 Bad Request\r\n" + textErrorContent
	textUpgradeRequired = "HTTP/1.1 426 Upgrade Required\r\n" + textErrorContent
	crlf                = "\r\n"
	colonAndSpace       = ": "
)

var (
	btsErrorVersion = []byte(headerSecVersion + ": 13\r\n")
)

var (
	ErrMalformedHttpRequest  = fmt.Errorf("malformed HTTP request")
	ErrMalformedHttpResponse = fmt.Errorf("malformed HTTP response")

	ErrBadHttpRequestProto  = fmt.Errorf("bad HTTP request protocol version")
	ErrBadHttpRequestMethod = fmt.Errorf("bad HTTP request method")

	ErrBadHost       = fmt.Errorf("bad %q header", headerHost)
	ErrBadUpgrade    = fmt.Errorf("bad %q header", headerUpgrade)
	ErrBadConnection = fmt.Errorf("bad %q header", headerConnection)
	ErrBadSecAccept  = fmt.Errorf("bad %q header", headerSecAccept)
	ErrBadSecKey     = fmt.Errorf("bad %q header", headerSecKey)
	ErrBadSecVersion = fmt.Errorf("bad %q header", headerSecVersion)
)

var (
	headerUpgrade       = textproto.CanonicalMIMEHeaderKey("Upgrade")
	headerConnection    = textproto.CanonicalMIMEHeaderKey("Connection")
	headerHost          = textproto.CanonicalMIMEHeaderKey("Host")
	headerOrigin        = textproto.CanonicalMIMEHeaderKey("Origin")
	headerSecVersion    = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Version")
	headerSecProtocol   = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Protocol")
	headerSecExtensions = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Extensions")
	headerSecKey        = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Key")
	headerSecAccept     = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Accept")
)

var (
	httpVersion1_0    = []byte("HTTP/1.0")
	httpVersion1_1    = []byte("HTTP/1.1")
	httpVersionPrefix = []byte("HTTP/")
)

type httpRequestLine struct {
	method, uri  []byte
	major, minor int
}

// httpParseRequestLine parses http request line like "GET / HTTP/1.0".
func httpParseRequestLine(line []byte) (req httpRequestLine, err error) {
	var proto []byte
	req.method, req.uri, proto = bsplit3(line, ' ')

	var ok bool
	req.major, req.minor, ok = httpParseVersion(proto)
	if !ok {
		err = ErrMalformedHttpRequest
		return
	}

	return
}

// httpParseVersion parses major and minor version of HTTP protocol. It returns
// parsed values and true if parse is ok.
func httpParseVersion(bts []byte) (major, minor int, ok bool) {
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

// httpParseHeaderLine parses HTTP header as key-value pair. It returns parsed
// values and true if parse is ok.
func httpParseHeaderLine(line []byte) (k, v []byte, ok bool) {
	colon := bytes.IndexByte(line, ':')
	if colon == -1 {
		return
	}

	k = btrim(line[:colon])
	// TODO(gobwas): maybe use just lower here?
	canonicalizeHeaderKey(k)

	v = btrim(line[colon+1:])

	return k, v, true
}

// httpGetHeader is the same as textproto.MIMEHeader.Get, except the thing,
// that key is already canonical. This helps to increase performance.
func httpGetHeader(h http.Header, key string) string {
	if h == nil {
		return ""
	}
	v := h[key]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// The request MAY include a header field with the name
// |Sec-WebSocket-Protocol|.  If present, this value indicates one or more
// comma-separated subprotocol the client wishes to speak, ordered by
// preference.  The elements that comprise this value MUST be non-empty strings
// with characters in the range U+0021 to U+007E not including separator
// characters as defined in [RFC2616] and MUST all be unique strings.  The ABNF
// for the value of this header field is 1#token, where the definitions of
// constructs and rules are as given in [RFC2616].
func strSelectProtocol(h string, check func(string) bool) (ret string, ok bool) {
	ok = httphead.ScanTokens(strToBytes(h), func(v []byte) bool {
		if check(btsToString(v)) {
			ret = string(v)
			return false
		}
		return true
	})
	return
}
func btsSelectProtocol(h []byte, check func([]byte) bool) (ret string, ok bool) {
	var selected []byte
	ok = httphead.ScanTokens(h, func(v []byte) bool {
		if check(v) {
			selected = v
			return false
		}
		return true
	})
	if ok && selected != nil {
		return string(selected), true
	}
	return
}

func strSelectExtensions(h string, selected []httphead.Option, check func(httphead.Option) bool) ([]httphead.Option, bool) {
	return btsSelectExtensions(strToBytes(h), selected, check)
}
func btsSelectExtensions(h []byte, selected []httphead.Option, check func(httphead.Option) bool) ([]httphead.Option, bool) {
	s := httphead.OptionSelector{
		Flags: httphead.SelectUnique | httphead.SelectCopy,
		Check: check,
	}
	return s.Select(h, selected)
}

func httpWriteHeader(bw *bufio.Writer, key, value string) {
	httpWriteHeaderKey(bw, key)
	bw.WriteString(value)
	bw.WriteString(crlf)
}

func httpWriteHeaderKey(bw *bufio.Writer, key string) {
	bw.WriteString(key)
	bw.WriteString(colonAndSpace)
}

func httpWriteResponseUpgrade(bw *bufio.Writer, nonce [nonceSize]byte, hs Handshake, hw func(io.Writer)) {
	bw.WriteString(textUpgrade)

	httpWriteHeaderKey(bw, headerSecAccept)
	writeAccept(bw, nonce)
	bw.WriteString(crlf)

	if hs.Protocol != "" {
		httpWriteHeader(bw, headerSecProtocol, hs.Protocol)
	}
	if len(hs.Extensions) > 0 {
		httpWriteHeaderKey(bw, headerSecExtensions)
		httphead.WriteOptions(bw, hs.Extensions)
		bw.WriteString(crlf)
	}
	if hw != nil {
		hw(bw)
	}

	bw.WriteString(crlf)
}

func httpWriteResponseError(bw *bufio.Writer, err error, code int, hw func(io.Writer)) {
	switch code {
	case http.StatusBadRequest:
		bw.WriteString(textBadRequest)
	case http.StatusUpgradeRequired:
		bw.WriteString(textUpgradeRequired)
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

// HeaderWriter creates callback function that will dump h into recevied
// io.Writer inside created callback.
func HeaderWriter(h http.Header) func(io.Writer) {
	return func(w io.Writer) {
		h.Write(w)
	}
}
