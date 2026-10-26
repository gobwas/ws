/*
Package wsutil provides utilities for working with WebSocket protocol.

Overview:

	// Read masked text message from peer and check utf8 encoding.
	header, err := ws.ReadHeader(conn)
	if err != nil {
		// handle err
	}

	// Prepare to read payload.
	r := io.LimitReader(conn, header.Length)
	r = wsutil.NewCipherReader(r, header.Mask)
	r = wsutil.NewUTF8Reader(r)

	payload, err := io.ReadAll(r)
	if err != nil {
		// handle err
	}

You could get the same behavior using just `wsutil.Reader`:

	r := wsutil.Reader{
		Source:    conn,
		CheckUTF8: true,
	}

	payload, err := io.ReadAll(r)
	if err != nil {
		// handle err
	}

Or even simplest:

	payload, err := wsutil.ReadClientText(conn)
	if err != nil {
		// handle err
	}

Limits:

The protocol has no limit on the size of a frame or a message, and neither has
this package by default, while ReadMessage(), ReadData() and their shortcuts
such as ReadClientText() read a whole message into memory. It is highly
recommended to set the limits that fit your application, once, before reading:

	// Reject a message, in all its fragments, bigger than 1 MiB.
	wsutil.SetMaxMessageSize(1 << 20)
	// Reject a single frame bigger than 64 KiB.
	wsutil.SetMaxFrameSize(64 << 10)

The same limits are the MaxMessageSize and MaxFrameSize fields of a Reader you
build yourself; DefaultReader() returns the Reader the helpers read with. The
declared length of a frame is never trusted for allocation beyond
PreallocLimit(), 32 KiB by default, so a peer that lies about the length can
not make you allocate what it has not sent.

Package is also exports tools for buffered writing:

	// Create buffered writer, that will buffer output bytes and send them as
	// 128-length fragments (with exception on large writes, see the doc).
	writer := wsutil.NewWriterSize(conn, ws.StateServerSide, ws.OpText, 128)

	_, err := io.CopyN(writer, rand.Reader, 100)
	if err == nil {
		err = writer.Flush()
	}
	if err != nil {
		// handle error
	}

For more utils and helpers see the documentation.
*/
package wsutil
