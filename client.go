package ws

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gobwas/httphead"
	"github.com/gobwas/pool/pbufio"
)

// ReaderPool describes object that manages reuse of bufio.Reader instances.
type ReaderPool interface {
	Get(io.Reader) *bufio.Reader
	Put(*bufio.Reader)
}

// WriterPool describes object that manages reuse of bufio.Writer instances.
type WriterPool interface {
	Get(io.Writer) *bufio.Writer
	Put(*bufio.Writer)
}

// Handshake represents handshake result.
type Handshake struct {
	// Protocol is the selected during handshake subprotocol.
	Protocol string

	// Extensions is the list of negotiated extensions.
	Extensions []httphead.Option
}

// Response represents result of dialing.
type Response struct {
	*http.Response
	Handshake
}

var (
	defaultWriterPool = writerPool(512)
	defaultReaderPool = readerPool(512)
)

// Errors used by the websocket client.
var (
	ErrBadStatus      = fmt.Errorf("unexpected http status")
	ErrBadSubProtocol = fmt.Errorf("unexpected protocol in %q header", headerSecProtocol)
	ErrBadExtensions  = fmt.Errorf("unexpected extensions in %q header", headerSecProtocol)
)

// DefaultDialer is dialer that holds no options and is used by Dial function.
var DefaultDialer Dialer

// Dial is like Dialer{}.Dial().
func Dial(ctx context.Context, urlstr string, h http.Header) (conn net.Conn, resp Response, err error) {
	return DefaultDialer.Dial(ctx, urlstr, h)
}

// Dialer contains options for establishing websocket connection to an url.
type Dialer struct {
	// Protocol is the list of subprotocol names the client wishes to speak, ordered by preference.
	// See https://tools.ietf.org/html/rfc6455#section-4.1
	Protocol []string

	// Extensions is the list of extensions, that client wishes to speak.
	// See https://tools.ietf.org/html/rfc6455#section-4.1
	// See https://tools.ietf.org/html/rfc6455#section-9.1
	Extensions []httphead.Option

	// NetDial is the function that is used to get plain tcp connection.
	// If it is not nil, then it is used instead of net.Dialer.
	NetDial func(ctx context.Context, network, addr string) (net.Conn, error)

	// NetDialTLS is the function that is used to get plain tcp connection with tls encryption.
	// If it is not nil, then it is used instead of tls.DialWithDialer.
	NetDialTLS func(ctx context.Context, network, addr string) (net.Conn, error)

	// TLSConfig is passed to tls.DialWithDialer.
	TLSConfig *tls.Config

	// WriterPool is used to reuse bufio.Writers.
	WriterPool WriterPool

	// ReaderPool is used to reuse bufio.Readers.
	ReaderPool ReaderPool
}

// Dial connects to the url host and handshakes connection to websocket.
// Set of additional headers could be passed to be sent with the request.
func (d Dialer) Dial(ctx context.Context, urlstr string, h http.Header) (conn net.Conn, resp Response, fb []byte, err error) {
	req := getRequest()
	defer putRequest(req)

	err = req.Reset(urlstr, h, d.Protocol, d.Extensions)
	if err != nil {
		return
	}

	conn, err = d.dial(ctx, req.URL)
	if err != nil {
		return
	}

	resp.Response, fb, err = d.do(ctx, conn, req)
	if err != nil {
		return
	}
	resp.Protocol, resp.Extensions, err = d.checkHandshake(req, resp)

	return
}

func (d Dialer) dial(ctx context.Context, u *url.URL) (conn net.Conn, err error) {
	addr := hostport(u)
	if u.Scheme == "wss" {
		if nd := d.NetDialTLS; nd != nil {
			return nd(ctx, "tcp", addr)
		}

		var nd net.Dialer
		if deadline, ok := ctx.Deadline(); ok {
			nd.Deadline = deadline
		}
		return tls.DialWithDialer(&nd, "tcp", addr, d.TLSConfig)
	}

	if nd := d.NetDial; nd != nil {
		return nd(ctx, "tcp", addr)
	}

	var nd net.Dialer
	return nd.DialContext(ctx, "tcp", addr)
}

var (
	// This variables are set like in net/net.go.
	// noDeadline is just zero value for readability.
	noDeadline = time.Time{}
	// aLongTimeAgo is a non-zero time, far in the past, used for immediate
	// cancelation of dials.
	aLongTimeAgo = time.Unix(42, 0)
)

// do sends request to the given connection and reads a request.
// It returns response and some bytes which could be written by the peer right
// after response and be caught by us during buffered read.
func (d Dialer) do(ctx context.Context, conn net.Conn, req *request) (resp *http.Response, fb []byte, err error) {
	var (
		wp WriterPool
		rp ReaderPool
	)
	if wp = d.WriterPool; wp == nil {
		wp = defaultWriterPool
	}
	if rp = d.ReaderPool; rp == nil {
		rp = defaultReaderPool
	}

	if deadline, _ := ctx.Deadline(); !deadline.IsZero() {
		conn.SetDeadline(deadline)
		defer conn.SetDeadline(noDeadline)
	}
	// If ctx is not a background, then it could be canceled. So we need to
	// handle this cancelation properly.
	if ctx != context.Background() {
		var (
			done      = make(chan struct{})
			interrupt = make(chan error, 1)
		)
		defer func() {
			close(done)
			if ctxErr := <-interrupt; ctxErr != nil && err == nil {
				err = ctxErr
				resp = nil
				fb = nil
			}
		}()
		// TODO(gobwas): use goroutine pool here maybe?
		go func() {
			select {
			case <-done:
				interrupt <- nil
			case <-ctx.Done():
				// Cancel io immediately.
				conn.SetDeadline(aLongTimeAgo)
				interrupt <- ctx.Err()
			}
		}()
	}

	bw := wp.Get(conn)
	defer wp.Put(bw)
	if err = req.Write(bw); err != nil {
		return
	}
	if err = bw.Flush(); err != nil {
		return
	}

	br := rp.Get(conn)
	defer rp.Put(br)
	resp, err = http.ReadResponse(br, nil)

	if err == nil && br.Buffered() != 0 {
		// Server has written frame bytes to the connection.
		// To not loose them we must read them.
		fb = make([]byte, br.Buffered())
		_, err = br.Read(fb)
	}

	return
}

func (d Dialer) checkHandshake(req *request, resp Response) (protocol string, extensions []httphead.Option, err error) {
	if resp.StatusCode != 101 {
		err = ErrBadStatus
		return
	}
	if u := resp.Header.Get(headerUpgrade); u != "websocket" && !strEqualFold(u, "websocket") {
		err = ErrBadUpgrade
		return
	}
	if c := resp.Header.Get(headerConnection); c != "Upgrade" && !strHasToken(c, "upgrade") {
		err = ErrBadConnection
		return
	}
	if !checkNonce(resp.Header.Get(headerSecAccept), req.Nonce) {
		err = ErrBadSecAccept
		return
	}

	for _, ext := range resp.Header[headerSecExtensions] {
		var ok bool
		extensions, ok = httphead.ParseOptions([]byte(ext), extensions)
		if !ok {
			err = ErrMalformedHttpResponse
			return
		}
	}
	for _, ext := range extensions {
		var ok bool
		for _, want := range req.Extensions {
			if ext.Equal(want) {
				ok = true
				break
			}
		}
		if !ok {
			err = ErrBadExtensions
			return
		}
	}

	// We check single value of Sec-Websocket-Protocol header according to this:
	// RFC6455 1.3:  "The server selects one or none of the acceptable protocols and echoes
	// that value in its handshake to indicate that it has selected that
	// protocol."
	if protocol = resp.Header.Get(headerSecProtocol); protocol != "" {
		var has bool
		for _, p := range req.Protocols {
			if has = p == protocol; has {
				break
			}
		}
		if !has {
			err = ErrBadSubProtocol
			return
		}
	}
	return
}

type readerPool int

func (n readerPool) Get(r io.Reader) *bufio.Reader { return pbufio.GetReader(r, int(n)) }
func (n readerPool) Put(r *bufio.Reader)           { pbufio.PutReader(r) }

type writerPool int

func (n writerPool) Get(w io.Writer) *bufio.Writer { return pbufio.GetWriter(w, int(n)) }
func (n writerPool) Put(w *bufio.Writer)           { pbufio.PutWriter(w) }

type timeoutError struct{}

func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
func (timeoutError) Error() string   { return "client timeout" }
