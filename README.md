# ws

[![GoDoc][godoc-image]][godoc-url]
[![Travis][travis-image]][travis-url]

> [RFC6455][rfc-url] WebSocket implementation in Go.

# Features

- Zero-copy upgrade
- No intermediate allocations during I/O
- Low-level API which allows to build your own logic of packet handling and buffers reuse
- High-level wrappers and helpers around API in `wsutil` package, which allow
  to start fast without digging the protocol internals

# Documentation

[GoDoc][godoc-url].

# Why

Existing WebSocket implementations do not allow users to reuse I/O buffers between
connections in clear way. This library aims to export efficient low-level interface for
working with the protocol without forcing only one way it could be used.

By the way, if you want get the higher-level tools, you can use `wsutil` sub-package.

# Status

This implementation of RFC6455 passes [Autobahn Test Suite](https://github.com/crossbario/autobahn-testsuite) and currently has
71.6% coverage (see `example/autobahn` folder for details).

The library is not tagged as `v1.0.0` yet so it can be broken during some
improvements or refactoring.

# Examples

Example applications using `ws` are developed in separate repository [ws-examples](https://github.com/gobwas/ws-examples).

# Usage

The higher-level example of WebSocket echo server:

```go
package main

import (
	"net/http"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func main() {
	http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
		if err != nil {
			// handle error
		}

		go func() {
			defer conn.Close()

			for {
				msg, op, err := wsutil.ReadClientData(conn)
				if err != nil {
					// handle error
				}
				err = wsutil.WriteServerMessage(conn, op, msg)
				if err != nil {
					// handle error
				}
			}
		}()
	}))
}
```

Lower-level, but still high-level example:


```go
import (
	"net/http"
	"io"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func main() {
	http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
		if err != nil {
			// handle error
		}

		go func() {
			defer conn.Close()

			var (
				state  = ws.StateServerSide
				reader = wsutil.NewReader(conn, state)
				writer = wsutil.NewWriter(conn, state, ws.OpText)
			)
			for {
				header, err := reader.NextFrame()
				if err != nil {
					// handle error
				}

				// Reset writer to write frame with right operation code.
				writer.Reset(conn, state, header.OpCode)

				if _, err = io.Copy(writer, reader); err != nil {
					// handle error
				}

				if err = writer.Flush(); err != nil {
					// handle error
				}
			}
		}()
	}))
}
```

The lower-level example without `wsutil`:

```go
package main

import (
	"net"
	"io"

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
		_, err = ws.Upgrade(conn)
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

				payload := make([]byte, header.Length)
				_, err = io.ReadFull(conn, payload)
				if err != nil {
					// handle error
				}
				if header.Masked {
					ws.Cipher(payload, header.Mask, 0)
				}

				// Reset the Masked flag, server frames must not be masked as
				// RFC6455 says.
				header.Masked = false

				if err := ws.WriteHeader(conn, header); err != nil {
					// handle error
				}
				if _, err := conn.Write(payload); err != nil {
					// handle error
				}

				if header.OpCode == ws.OpClose {
					return
				}
			}
		}()
	}
}
```

# Zero-copy upgrade

Zero-copy upgrade helps to avoid unnecessary allocations and copying while
handling HTTP Upgrade request.

Processing of all non-websocket headers is made in place with use of registered
user callbacks whose arguments are only valid until callback returns.

The simple example looks like this:

```go
package main

import (
	"net"
	"log"

	"github.com/gobwas/ws"
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

		_, err = u.Upgrade(conn)
		if err != nil {
			// handle error
		}
	}
}
```

Usage of `ws.Upgrader` here brings ability to control incoming connections on tcp
level and simply not to accept them by some logic.

Zero-copy upgrade is for high-load services which have to control many
resources such as connections buffers.

The real life example could be like this:

```go
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		// handle error
	}

	header := http.Header{
		"X-Go-Version": []string{runtime.Version()},
	}

	u := ws.Upgrader{
		OnRequest: func(host, uri []byte) (err error, code int) {
			if string(host) == "github.com" {
				return fmt.Errorf("unexpected host: %s", host), 403
			}
			return
		},
		OnHeader: func(key, value []byte) (err error, code int) {
			if string(key) != "Cookie" {
				return
			}
			ok := httphead.ScanCookie(value, func(key, value []byte) bool {
				// Check session here or do some other stuff with cookies.
				// Maybe copy some values for future use.
				return true
			})
			if !ok {
				return fmt.Errorf("bad cookie"), 400
			}
			return
		},
		BeforeUpgrade: func() (headerWriter func(io.Writer), err error, code int) {
			return ws.HeaderWriter(header), nil, 0
		},
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		_, err = u.Upgrade(conn)
		if err != nil {
			log.Printf("upgrade error: %s", err)
		}
	}
}
```



[rfc-url]: https://tools.ietf.org/html/rfc6455
[godoc-image]: https://godoc.org/github.com/gobwas/ws?status.svg
[godoc-url]: https://godoc.org/github.com/gobwas/ws
[travis-image]: https://travis-ci.org/gobwas/ws.svg?branch=master
[travis-url]: https://travis-ci.org/gobwas/ws
