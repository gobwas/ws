package ws

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	_ "unsafe" // for go:linkname
)

const (
	textUpgrade   = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
	crlf          = "\r\n"
	colonAndSpace = ": "
)

// Errors used by upgraders.
var (
	ErrBadHost       = fmt.Errorf("bad %q header", headerHost)
	ErrBadUpgrade    = fmt.Errorf("bad %q header", headerUpgrade)
	ErrBadConnection = fmt.Errorf("bad %q header", headerConnection)
	ErrBadSecAccept  = fmt.Errorf("bad %q header", headerSecAccept)
	ErrBadSecKey     = fmt.Errorf("bad %q header", headerSecKey)
	ErrBadSecVersion = fmt.Errorf("bad %q header", headerSecVersion)
	ErrBadHijacker   = fmt.Errorf("given http.ResponseWriter is not a http.Hijacker")
)

// SelectFromSlice creates accept function that could be used as Protocol/Extension
// select function in the UpgradeConfig.
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

// UpgradeConfig contains options for upgrading http connection to websocket.
type UpgradeConfig struct {
	// Header is the set of custom headers that will be sent with the response.
	Header http.Header

	// Protocol is the select function that is used to select subprotocol
	// from client passed list.
	Protocol func(string) bool

	// Extension is the select function that is used to select extensions
	// from client passed list.
	Extension func(string) bool
}

// Upgrade upgrades http connection to websocket.
// It hijacks net.Conn from response writer.
//
// If succeed it returns upgraded connection and Handshake struct describing
// handshake info.
func Upgrade(r *http.Request, w http.ResponseWriter, c *UpgradeConfig) (conn net.Conn, rw *bufio.ReadWriter, hs Handshake, err error) {
	if r.Host == "" {
		err = ErrBadHost
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if u := getHeader(r.Header, headerUpgrade); u != "websocket" && strings.ToLower(u) != "websocket" {
		err = ErrBadUpgrade
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if c := getHeader(r.Header, headerConnection); c != "Upgrade" && !hasToken(c, "upgrade") {
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

	rw.WriteString(textUpgrade)

	accept := makeAccept(strToBytes(nonce))
	writeHeaderKey(rw.Writer, headerSecAccept)
	writeHeaderValueBytes(rw.Writer, accept)

	if c != nil {
		if p, check := r.Header[headerSecProtocol], c.Protocol; len(p) > 0 && check != nil {
			for _, v := range p {
				if check(v) {
					hs.Protocol = v
					writeHeader(rw.Writer, headerSecProtocol, hs.Protocol)
					break
				}
			}
		}
		// TODO(gobwas) parse extensions.
		//if e, check := r.Header[headerSecExtensions], c.Extension; len(e) > 0 && check != nil {
		//	hs.Extensions = selectExtensions(e, c.Extension)
		//	if len(hs.Extensions) > 0 {
		//		writeHeader(rw.Writer, headerSecExtensions, strings.Join(hs.Extensions, ", "))
		//	}
		//}
		for key, values := range c.Header {
			for _, val := range values {
				writeHeader(rw.Writer, key, val)
			}
		}
	}

	rw.WriteString(crlf)

	err = rw.Flush()

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

func hasToken(header, token string) bool {
	var pos int
	for i := 0; i <= len(header); i++ {
		if i == len(header) || header[i] == ',' {
			v := strings.TrimSpace(header[pos:i])
			if len(v) == len(token) && strings.ToLower(v) == token {
				return true
			}
			pos = i + 1
		}
	}
	return false
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
