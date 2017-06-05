# ws

[![GoDoc][godoc-image]][godoc-url] [![Build Status][travis-image]][travis-url]

> [RFC6455][rfc-url] WebSocket implementation in Go.

# Features

- Zero-copy upgrade
- No intermediate allocations during IO
- Low-level API which allows to build your own packet handling and buffers
  reuse

# Usage

The non-optimized usage example could look like this:

```go

import (
	"net/http"
	
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func main() {
	http.HandleFunc("/websocket", func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.Upgrade(r, w, nil)
		if err != nil {
			// Handle error some how.
		}
		
		go func() {
			for {
				f, err := ws.ReadFrame(conn)
				if err != nil {
					// Handle read frame error.
				}
				err = ws.WriteFrame(conn, ws.NewTextFrame("hello there!"))
				if err != nil {
					// Handle write frame error.
				}
			}
		}()
	})
}

```

# Zero-copy upgrade

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

	var (
		expectHost = "github.com"
		expectURI  = "/websocket"
	)

	reqID := []string{"0"}
	header := http.Header{
		"X-Request-ID": reqID,
	}

	u := ws.ConnUpgrader{
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

	for id := 0;; id++ {
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

# Why

Current WebSocket implementations does not allows to use low-level
optimizations such reusing buffers between multiple connections and so on.

I was looking for tiny RFC6455 implementation that could be used like
`ReadFrame()` or `WriteFrame()` but no libraries was found.

# Status

This implementation of RFC6455 is passes [Autobahn Test Suite](https://github.com/crossbario/autobahn-testsuite) and currently has
71.6% coverage.

The library is not tagged as `v1.0.0` yet so it could be broken during some improvements
or refactoring.



[rfc-url]: https://tools.ietf.org/html/rfc6455
[godoc-image]: https://godoc.org/github.com/gobwas/ws?status.svg
[godoc-url]: https://godoc.org/github.com/gobwas/ws
[travis-image]: https://travis-ci.org/gobwas/ws.svg?branch=master
[travis-url]: https://travis-ci.org/gobwas/ws
