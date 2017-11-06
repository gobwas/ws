package ws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gobwas/httphead"
	"github.com/gobwas/pool/pbufio"
)

// Constants used by Dialer.
const (
	DefaultClientReadBufferSize  = 4096
	DefaultClientWriteBufferSize = 4096
)

// Handshake represents handshake result.
type Handshake struct {
	// Protocol is the subprotocol selected during handshake.
	Protocol string

	// Extensions is the list of negotiated extensions.
	Extensions []httphead.Option
}

// Errors used by the websocket client.
var (
	ErrHandshakeBadStatus      = fmt.Errorf("unexpected http status")
	ErrHandshakeBadSubProtocol = fmt.Errorf("unexpected protocol in %q header", headerSecProtocol)
	ErrHandshakeBadExtensions  = fmt.Errorf("unexpected extensions in %q header", headerSecProtocol)
)

// DefaultDialer is dialer that holds no options and is used by Dial function.
var DefaultDialer Dialer

// Dial is like Dialer{}.Dial().
func Dial(ctx context.Context, urlstr string) (net.Conn, *bufio.Reader, Handshake, error) {
	return DefaultDialer.Dial(ctx, urlstr)
}

// Dialer contains options for establishing websocket connection to an url.
type Dialer struct {
	// ReadBufferSize and WriteBufferSize is an I/O buffer sizes.
	// They used to read and write http data while upgrading to WebSocket.
	// Allocated buffers are pooled with sync.Pool to avoid extra allocations.
	//
	// If a size is zero then default value is used.
	ReadBufferSize, WriteBufferSize int

	// Timeout is the maximum amount of time a Dial() will wait for a connect
	// and an handshake to complete.
	//
	// The default is no timeout.
	Timeout time.Duration

	// Protocols is the list of subprotocols that the client wants to speak,
	// ordered by preference.
	//
	// See https://tools.ietf.org/html/rfc6455#section-4.1
	Protocols []string

	// Extensions is the list of extensions that client wants to speak.
	//
	// Note that if server decides to use some of this extensions, Dial() will
	// return Handshake struct containing a slice of items, which are the
	// shallow copies of the items from this list. That is, internals of
	// Extensions items are shared during Dial().
	//
	// See https://tools.ietf.org/html/rfc6455#section-4.1
	// See https://tools.ietf.org/html/rfc6455#section-9.1
	Extensions []httphead.Option

	// Header is the callback that will be called with io.Writer.
	// Write() calls to the given writer will put data in a request http
	// headers section.
	//
	// It used instead of http.Header mapping to avoid allocations in user land.
	Header func(io.Writer)

	// OnStatusError is the callback that will be called after receiving non
	// "101 Continue" HTTP response status. It receives an io.Reader object
	// representing server response bytes. That is, it gives ability to parse
	// HTTP response somehow (probably with http.ReadResponse call) and make a
	// decision of further logic.
	//
	// The arguments are only valid until the callback returns.
	OnStatusError func(status int, reason []byte, resp io.Reader)

	// OnHeader is the callback that will be called after successful parsing of
	// header, that is not used during WebSocket handshake procedure. That is,
	// it will be called with non-websocket headers, which could be relevant
	// for application-level logic.
	//
	// The arguments are only valid until the callback returns.
	//
	// Returned value could be used to prevent processing response.
	OnHeader func(key, value []byte) (err error)

	// NetDial is the function that is used to get plain tcp connection.
	// If it is not nil, then it is used instead of net.Dialer.
	NetDial func(ctx context.Context, network, addr string) (net.Conn, error)

	// TLSClient is the callback that will be called after succesful dial with
	// received connection and its remote host name. If it is nil, then the
	// default tls.Client() will be used.
	// If it is not nil, then TLSConfig field is ignored.
	TLSClient func(conn net.Conn, hostname string) net.Conn

	// TLSConfig is passed to tls.Client() to start TLS over established
	// connection. If TLSClient is not nil, then it is ignored. If TLSConfig is
	// non-nil and its ServerName is empty, then for every Dial() it will be
	// cloned and appropriate ServerName will be set.
	TLSConfig *tls.Config

	// WrapConn is the optional callback that will be called when connection is
	// ready for an i/o. That is, it will be called after successful dial and
	// TLS initialization (for "wss" schemes). It may be helpful for different
	// user land purposes such as end to end encryption.
	//
	// Note that for debugging purposes of an http handshake (e.g. sent request
	// and received response), there is an wsutil.DebugDialer struct.
	WrapConn func(conn net.Conn) net.Conn
}

// Dial connects to the url host and upgrades connection to WebSocket.
//
// If server has sent frames right after successful handshake then returned
// buffer will be non-nil. In other cases buffer is always nil. For better
// memory efficiency received non-nil bufio.Reader should be returned to the
// inner pool with PutReader() function after use.
//
// Note that Dialer does not implement IDNA (RFC5895) logic as net/http does.
// If you want to dial non-ascii host name, take care of its name serialization
// avoiding bad request issues. For more info see net/http Request.Write()
// implementation, especially cleanHost() function.
//
// If you do not want to dial with RFC compliant url starting with "ws:" or
// "wss:" scheme (which is rare case, but possible), you can use url with
// custom scheme to specify a network name, and host or path as an address.
// That is, for "unix*" schemes address is an url's path, for other networks
// address is an url's host. For example, to dial unix domain socket you could
// pass urlstr as "unix:/var/run/app.sock". Note that it is not possible to use
// TLS for such connections.
func (d Dialer) Dial(ctx context.Context, urlstr string) (conn net.Conn, br *bufio.Reader, hs Handshake, err error) {
	u, err := url.ParseRequestURI(urlstr)
	if err != nil {
		return
	}
	if t := d.Timeout; t != 0 {
		deadline := time.Now().Add(t)
		if d, ok := ctx.Deadline(); !ok || deadline.Before(d) {
			subctx, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			ctx = subctx
		}
	}
	if conn, err = d.dial(ctx, u); err != nil {
		return
	}
	br, hs, err = d.request(ctx, conn, u)
	if err != nil {
		conn.Close()
	}
	return
}

var (
	// netEmptyDialer is a net.Dialer without options, used in Dialer.dial() if
	// Dialer.NetDial is not provided.
	netEmptyDialer net.Dialer
	// tlsEmptyConfig is an empty tls.Config used as default one.
	tlsEmptyConfig tls.Config
)

func tlsDefaultConfig() *tls.Config {
	return &tlsEmptyConfig
}

func hostport(host string, defaultPort string) (hostname, addr string) {
	var (
		colon   = strings.LastIndexByte(host, ':')
		bracket = strings.IndexByte(host, ']')
	)
	if colon > bracket {
		return host[:colon], host
	}
	return host, host + defaultPort
}

func (d Dialer) dial(ctx context.Context, u *url.URL) (conn net.Conn, err error) {
	dial := d.NetDial
	if dial == nil {
		dial = netEmptyDialer.DialContext
	}
	switch u.Scheme {
	case "ws":
		_, addr := hostport(u.Host, ":80")
		conn, err = dial(ctx, "tcp", addr)
	case "unix", "unixgram", "unixpacket":
		conn, err = dial(ctx, u.Scheme, u.Path)
	default:
		conn, err = dial(ctx, u.Scheme, u.Host)
	case "wss":
		hostname, addr := hostport(u.Host, ":443")
		conn, err = dial(ctx, "tcp", addr)
		if err != nil {
			return
		}
		tlsClient := d.TLSClient
		if tlsClient == nil {
			tlsClient = d.tlsClient
		}
		conn = tlsClient(conn, hostname)
	}
	if wrap := d.WrapConn; wrap != nil {
		conn = wrap(conn)
	}
	return
}

func (d Dialer) tlsClient(conn net.Conn, hostname string) net.Conn {
	config := d.TLSConfig
	if config == nil {
		config = tlsDefaultConfig()
	}
	if config.ServerName == "" {
		config = tlsCloneConfig(config)
		config.ServerName = hostname
	}
	// Do not make conn.Handshake() here because downstairs we will prepare
	// i/o on this conn with proper context's timeout handling.
	return tls.Client(conn, config)
}

var (
	// This variables are set like in net/net.go.
	// noDeadline is just zero value for readability.
	noDeadline = time.Time{}
	// aLongTimeAgo is a non-zero time, far in the past, used for immediate
	// cancelation of dials.
	aLongTimeAgo = time.Unix(42, 0)
)

// request sends request to the given connection and reads a response.
// It returns response and some bytes which could be written by the peer right
// after response and be caught by us during buffered read.
func (d Dialer) request(ctx context.Context, conn net.Conn, u *url.URL) (br *bufio.Reader, hs Handshake, err error) {
	// headerSeen constants helps to report whether or not some header was seen
	// during reading request bytes.
	const (
		headerSeenUpgrade = 1 << iota
		headerSeenConnection
		headerSeenSecAccept

		// headerSeenAll is the value that we expect to receive at the end of
		// headers read/parse loop.
		headerSeenAll = 0 |
			headerSeenUpgrade |
			headerSeenConnection |
			headerSeenSecAccept
	)

	if ctx != context.Background() {
		// Context could be canceled or its deadline could be exceeded.
		// Start the interrupter goroutine to handle context cancelation.
		var (
			done      = make(chan struct{})
			interrupt = make(chan error, 1)
		)
		defer func() {
			close(done)
			// If ctx.Err() is non-nil and the original err is net.Error with
			// Timeout() == true, then it means that i/o was canceled by us by
			// SetDeadline(aLongTimeAgo) call, or by somebody else previously
			// by conn.SetDeadline(x). In both cases, context is canceled too.
			// Even on race condition when both connection deadline (set not by
			// us) and request context are exceeded, we prefer ctx.Err() to be
			// returned just to be consistent.
			if ctxErr := <-interrupt; ctxErr != nil && (err == nil || isTimeoutError(err)) {
				err = ctxErr
				if br != nil {
					pbufio.PutReader(br)
					br = nil
				}
			}
		}()
		// TODO(gobwas): use goroutine pool here maybe?
		go func() {
			select {
			case <-done:
				interrupt <- nil
			case <-ctx.Done():
				// Cancel i/o immediately.
				conn.SetDeadline(aLongTimeAgo)
				interrupt <- ctx.Err()
			}
		}()
	}

	bw := pbufio.GetWriter(conn,
		nonZero(d.WriteBufferSize, DefaultClientWriteBufferSize),
	)
	defer pbufio.PutWriter(bw)

	// Stick nonce bytes to the stack.
	var n nonce
	nonce := n.bytes()
	initNonce(nonce)

	httpWriteUpgradeRequest(bw, u, nonce, d.Protocols, d.Extensions, d.Header)
	if err = bw.Flush(); err != nil {
		return
	}

	br = pbufio.GetReader(conn,
		nonZero(d.ReadBufferSize, DefaultClientReadBufferSize),
	)
	defer func() {
		if br.Buffered() == 0 || err != nil {
			// Server does not wrote additional bytes to the connection or
			// error occurred. That is, no reason to return buffer.
			pbufio.PutReader(br)
			br = nil
		}
	}()
	// Read HTTP status line like "HTTP/1.1 101 Switching Protocols".
	sl, err := readLine(br)
	if err != nil {
		return
	}
	// Begin validation of the response.
	// See https://tools.ietf.org/html/rfc6455#section-4.2.2
	// Parse request line data like HTTP version, uri and method.
	resp, err := httpParseResponseLine(sl)
	if err != nil {
		return
	}
	// Even if RFC says "1.1 or higher" without mentioning the part of the
	// version, we apply it only to minor part.
	if resp.major != 1 || resp.minor < 1 {
		err = ErrHandshakeBadProtocol
		return
	}
	if resp.status != 101 {
		err = StatusError(resp.status)
		if onStatusError := d.OnStatusError; onStatusError != nil {
			// Invoke callback with multireader of status-line bytes br.
			onStatusError(resp.status, resp.reason,
				io.MultiReader(
					bytes.NewReader(sl),
					strings.NewReader(crlf),
					br,
				),
			)
		}
		return
	}
	// If response status is 101 then we expect all technical headers to be
	// valid. If not, then we stop processing response without giving user
	// ability to read non-technical headers. That is, we do not distinguish
	// technical errors (such as parsing error) and protocol errors.
	var headerSeen byte
	for {
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
			err = ErrMalformedResponse
			return
		}

		switch btsToString(k) {
		case headerUpgrade:
			headerSeen |= headerSeenUpgrade
			if !bytes.Equal(v, specHeaderValueUpgrade) && !btsEqualFold(v, specHeaderValueUpgrade) {
				err = ErrHandshakeBadUpgrade
				return
			}

		case headerConnection:
			headerSeen |= headerSeenConnection
			// Note that as RFC6455 says:
			//   > A |Connection| header field with value "Upgrade".
			// That is, in server side, "Connection" header could contain
			// multiple token. But in response it must contains exactly one.
			if !bytes.Equal(v, specHeaderValueConnection) && !btsEqualFold(v, specHeaderValueConnection) {
				err = ErrHandshakeBadConnection
				return
			}

		case headerSecAccept:
			headerSeen |= headerSeenSecAccept
			if !checkAcceptFromNonce(v, nonce) {
				err = ErrHandshakeBadSecAccept
				return
			}

		case headerSecProtocol:
			// RFC6455 1.3:
			//   "The server selects one or none of the acceptable protocols
			//   and echoes that value in its handshake to indicate that it has
			//   selected that protocol."
			for _, want := range d.Protocols {
				if string(v) == want {
					hs.Protocol = want
					break
				}
			}
			if hs.Protocol == "" {
				// Server echoed subprotocol that is not present in client
				// requested protocols.
				err = ErrHandshakeBadSubProtocol
				return
			}

		case headerSecExtensions:
			hs.Extensions, err = matchSelectedExtensions(v, d.Extensions, hs.Extensions)
			if err != nil {
				return
			}

		default:
			if onHeader := d.OnHeader; onHeader != nil {
				if e := onHeader(k, v); e != nil {
					err = e
					return
				}
			}
		}
	}
	if err == nil && headerSeen != headerSeenAll {
		switch {
		case headerSeen&headerSeenUpgrade == 0:
			err = ErrHandshakeBadUpgrade
		case headerSeen&headerSeenConnection == 0:
			err = ErrHandshakeBadConnection
		case headerSeen&headerSeenSecAccept == 0:
			err = ErrHandshakeBadSecAccept
		default:
			panic("unknown headers state")
		}
	}
	return
}

// PutReader returns bufio.Reader instance to the inner reuse pool.
// It is useful in rare cases, when Dialer.Dial() returns non-nil buffer which
// contains unprocessed buffered data, that was sent by the server quickly
// right after handshake.
func PutReader(br *bufio.Reader) {
	pbufio.PutReader(br)
}

type timeoutError struct{}

func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
func (timeoutError) Error() string   { return "client timeout" }

// StatusError contains an unexpected status-line code from the server.
type StatusError int

func (s StatusError) Error() string {
	return "unexpected HTTP response status: " + strconv.Itoa(int(s))
}

func isTimeoutError(err error) bool {
	t, ok := err.(net.Error)
	return ok && t.Timeout()
}

func matchSelectedExtensions(selected []byte, wanted, received []httphead.Option) ([]httphead.Option, error) {
	if len(selected) == 0 {
		return received, nil
	}
	var (
		index  int
		option httphead.Option
		err    error
	)
	index = -1
	match := func() (ok bool) {
		for _, want := range wanted {
			if option.Equal(want) {
				// Check parsed extension to be present in client
				// requested extensions. We move matched extension
				// from client list to avoid allocation.
				received = append(received, want)
				return true
			}
		}
		return false
	}
	ok := httphead.ScanOptions(selected, func(i int, name, attr, val []byte) httphead.Control {
		if i != index {
			// Met next option.
			index = i
			if i != 0 && !match() {
				// Server returned non-requested extension.
				err = ErrHandshakeBadExtensions
				return httphead.ControlBreak
			}
			option = httphead.Option{Name: name}
		}
		if attr != nil {
			option.Parameters.Set(attr, val)
		}
		return httphead.ControlContinue
	})
	if !ok {
		err = ErrMalformedResponse
		return received, err
	}
	if !match() {
		return received, ErrHandshakeBadExtensions
	}
	return received, err
}
