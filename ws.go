/*
Package ws implements a client and server for the WebSocket protocol as
specified in RFC 6455.

The main purpose of this package is to provide simple low-level API for
efficient work with protocol.

Overview

  // Upgrade raw net.Conn (or even io.ReadWriter) in zero-copy manner.
  handshake, err := ws.Upgrade(conn)
  if err != nil {
      // handle error
  }

  header, err := ws.ReadHeader(conn)
  if err != nil {
	  // handle err
  }

  buf := make([]byte, header.Length)
  _, err := io.ReadFull(conn, buf)
  if err != nil {
	  // handle err
  }

  resp := ws.NewBinaryFrame([]byte("hello, world!"))
  if err := ws.WriteFrame(conn, frame); err != nil {
      // handle err
  }

As you can see, it could be stream friendly:

  const N = 42

  ws.WriteHeader(ws.Header{
	  Fin:    true,
	  Length: 42,
	  OpCode: ws.OpBinary,
  })

  io.CopyN(conn, rand.Reader, N)

Or:

  header, err := ws.ReadHeader(conn)
  if err != nil {
	  // handle err
  }

  io.CopyN(ioutil.Discard, conn, header.Length)

For more info see the documentation.
*/
package ws
