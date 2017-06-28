# ws

[![GoDoc][godoc-image]][godoc-url] 

> [RFC6455][rfc-url] WebSocket implementation in Go.

# Features

- Zero-copy upgrade
- No intermediate allocations during I/O
- Low-level API which allows to build your own packet handling and buffers
  reuse

# Documentation

[GoDoc][godoc-url].

# Why

Existing WebSocket implementations does not allow to reuse I/O buffers between
connections in clear way. This library aims to export lower-level interface for
working with the protocol. Ideologically, the goal is not to force use patterns
of WebSocket entities.

By the way, if you want get the higher-level interface, you could use `wsutil`
sub-package.

# Usage

The higher-level example of WebSocket echo server:

```go
import (
	"net/http"
	
	"github.com/gobwas/ws"
	"github.com/gobwas/wsutil"
)

func main() {
	http.HandleFunc("/websocket", func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
		if err != nil {
			// handle error
		}

		go func() {
			defer conn.Close()

			state := ws.StateServer

			// Note that you could use bufio.{Reader, Writer} to reduce
			// syscalls.
			reader := wsutil.NewReader(conn, state)
			writer := wsutil.NewWriter(conn, ws.OpText, false)

			for {
				// Next() returns the next frame's header and makes reader
				// ready to read that frame.
				header, err := reader.Next()
				if err != nil {
					// handle error
				}

				// Handle control frames as spec says.
				if header.OpCode.IsControl() {
					err = wsutil.ControlHandler(conn, state)(header, reader)
					if err != nil {
						// handle error
					}
				}

				// Discard binary frame.
				if header.OpCode == ws.OpBinary {
					io.Copy(ioutil.Discard, reader)
					continue
				}

				// Otherwise echo text frame back to client (there are no more
				// other possible operation codes here).
				if _, err = io.Copy(writer, reader), err != nil {
					// handle error
				}
				if err = w.Flush(); err != nil {
					// handle error
				}
			}
		}()
	})
}
```

The lower-level example:

```go
import (
	"net/http"
	
	"github.com/gobwas/ws"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
		}

		_, err := ws.Upgrade(conn)
		if err != nil {
			// handle error
		}

		go func() {
			defer conn.Close()

			for {
				header, err := ws.ReadHeader(conn)
				if err != nil {
					// handle error
				}

				data, err := ioutil.ReadAll(conn, header.Length)
				if err != nil {
					// handle error
				}
				if header.Masked {
					ws.Cipher(data, header.Mask, 0)
				}

				// Echo text frame back to client.
				if header.OpCode == ws.OpText {
					
					err = ws.WriteHeader(conn, ws.Header{
						Fin:    true,
						OpCode: ws.OpText,
						Length: len(data),
					})
					if err == nil {
						_, err = conn.Write(data)
					}
					if err != nil {
						// handle error
					}
				}

				// handle control frames
			}
		}()
	}
}

```

# Zero-copy upgrade

Zero copy upgrade helps to avoid unnecessary allocations and copies while
handling HTTP Upgrade request.

Processing of all non-websocket headers is made in place with use of registered
user callbacks, when arguments are only valid until callback returns.

The simple example looks like this:

```go

import (
	"net"
	"net/http"
	"log"

	"github.com/gobwas/ws"
	"github.com/gobwas/httphead"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	u := ws.Upgrader{
		OnHeader: func(key, value []byte) (err error, code int) {
			log.Printf("non-websocket header: %q=%q", key, value)
			return
		},
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
		}

		_, err := u.Upgrade(conn)
		if err != nil {
			// handle error
		}
	}
}
```

Use of `ws.Upgrader` here brings ability to control incoming connections on tcp
level, and simply do not accept them by your custom logic.

Zero-copy upgrade are intended for high-load services with need to control many
resources such as alive connections and their buffers.

The real life example could be like this:

```go

import (
	"net"
	"net/http"
	"log"

	"github.com/gobwas/ws"
	"github.com/gobwas/httphead"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		// handle error
	}

	var (
		expectHost = "github.com"
		expectURI  = "/websocket"
	)

	var id int
	reqID := []string{"0"}
	header := http.Header{
		"X-Request-ID": reqID,
	}

	u := ws.Upgrader{
		OnRequest: func(host, uri []byte) (err error, code int) {
			if !bytes.Equal(host, expectHost) {
				return fmt.Errorf("unexpected host: %s", host), 403
			}
			if !bytes.Equal(uri, expectURI) {
				return fmt.Errorf("unexpected uri: %s", uri), 403
			}
			return // Continue upgrade.
		},
		OnHeader: func(key, value []byte) (err error, code int) {
			if !bytes.Equal(key, headerCookie) {
				return
			}
			cookieOK := httphead.ScanCookie(value, func(key, value []byte) bool {
				// Check session here or do some other stuff with cookies.
				// Maybe copy some values for future use.
			})
			if !cookieOK {
				return fmt.Errorf("bad cookie"), 400
			}
			return
		},
		BeforeUpgrade: func() (headerWriter func(io.Writer), err error, code int) {
			// Final checks here before return 101 Continue.
			
			reqID[0], err = strconv.FormatInt(id, 10)
			if err != nil {
				return nil, err, 500
			}
			
			return func(w io.Writer) {
				header.Write(w)
			}, nil, 0
		},
	}

	for ;; id++ {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		_, err := u.Upgrade(conn)
		if err != nil {
			log.Printf("upgrade error: %s", err)
		}
	}
}
```

# Status

This implementation of RFC6455 passes [Autobahn Test Suite](https://github.com/crossbario/autobahn-testsuite) and currently has
71.6% coverage.

The library is not tagged as `v1.0.0` yet so it could be broken during some improvements
or refactoring.



[rfc-url]: https://tools.ietf.org/html/rfc6455
[godoc-image]: https://godoc.org/github.com/gobwas/ws?status.svg
[godoc-url]: https://godoc.org/github.com/gobwas/ws
[travis-image]: https://travis-ci.org/gobwas/ws.svg?branch=master
[travis-url]: https://travis-ci.org/gobwas/ws
