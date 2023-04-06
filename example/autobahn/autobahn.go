package main

import (
	"compress/flate"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsflate"
	"github.com/gobwas/ws/wsutil"
)

var addr = flag.String("listen", ":9001", "addr to listen")

func main() {
	log.SetFlags(0)
	flag.Parse()

	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/wsutil", wsutilHandler)
	http.HandleFunc("/wsflate", wsflateHandler)
	http.HandleFunc("/helpers/low", helpersLowLevelHandler)
	http.HandleFunc("/helpers/high", helpersHighLevelHandler)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %q error: %v", *addr, err)
	}
	log.Printf("listening %s (%q)", ln.Addr(), *addr)

	var (
		s     = new(http.Server)
		serve = make(chan error, 1)
		sig   = make(chan os.Signal, 1)
	)
	signal.Notify(sig, syscall.SIGTERM)
	go func() { serve <- s.Serve(ln) }()

	select {
	case err := <-serve:
		log.Fatal(err)
	case sig := <-sig:
		const timeout = 5 * time.Second

		log.Printf("signal %q received; shutting down with %s timeout", sig, timeout)

		ctx, _ := context.WithTimeout(context.Background(), timeout)
		if err := s.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}
}

var (
	closeInvalidPayload = ws.MustCompileFrame(
		ws.NewCloseFrame(ws.NewCloseFrameBody(
			ws.StatusInvalidFramePayloadData, "",
		)),
	)
	closeProtocolError = ws.MustCompileFrame(
		ws.NewCloseFrame(ws.NewCloseFrameBody(
			ws.StatusProtocolError, "",
		)),
	)
)

func helpersHighLevelHandler(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		log.Printf("upgrade error: %s", err)
		return
	}
	defer conn.Close()

	for {
		bts, op, err := wsutil.ReadClientData(conn)
		if err != nil {
			log.Printf("read message error: %v", err)
			return
		}
		err = wsutil.WriteServerMessage(conn, op, bts)
		if err != nil {
			log.Printf("write message error: %v", err)
			return
		}
	}
}

func helpersLowLevelHandler(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		log.Printf("upgrade error: %s", err)
		return
	}
	defer conn.Close()

	msg := make([]wsutil.Message, 0, 4)

	for {
		msg, err = wsutil.ReadClientMessage(conn, msg[:0])
		if err != nil {
			log.Printf("read message error: %v", err)
			return
		}
		for _, m := range msg {
			if m.OpCode.IsControl() {
				err := wsutil.HandleClientControlMessage(conn, m)
				if err != nil {
					log.Printf("handle control error: %v", err)
					return
				}
				continue
			}
			err := wsutil.WriteServerMessage(conn, m.OpCode, m.Payload)
			if err != nil {
				log.Printf("write message error: %v", err)
				return
			}
		}
	}
}

func wsutilHandler(res http.ResponseWriter, req *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(req, res)
	if err != nil {
		log.Printf("upgrade error: %s", err)
		return
	}
	defer conn.Close()

	state := ws.StateServerSide

	ch := wsutil.ControlFrameHandler(conn, state)
	r := &wsutil.Reader{
		Source:         conn,
		State:          state,
		CheckUTF8:      true,
		OnIntermediate: ch,
	}
	w := wsutil.NewWriter(conn, state, 0)

	for {
		h, err := r.NextFrame()
		if err != nil {
			log.Printf("next frame error: %v", err)
			return
		}
		if h.OpCode.IsControl() {
			if err = ch(h, r); err != nil {
				log.Printf("handle control error: %v", err)
				return
			}
			continue
		}

		w.Reset(conn, state, h.OpCode)

		if _, err = io.Copy(w, r); err == nil {
			err = w.Flush()
		}
		if err != nil {
			log.Printf("echo error: %s", err)
			return
		}
	}
}

func wsflateHandler(w http.ResponseWriter, r *http.Request) {
	e := wsflate.Extension{
		Parameters: wsflate.Parameters{
			ServerNoContextTakeover: true,
			ClientNoContextTakeover: true,
		},
	}
	u := ws.HTTPUpgrader{
		Negotiate: e.Negotiate,
	}
	conn, _, _, err := u.Upgrade(r, w)
	if err != nil {
		log.Printf("upgrade error: %s", err)
		return
	}
	defer conn.Close()

	if _, ok := e.Accepted(); !ok {
		log.Printf("no accepted extension")
		return
	}

	// Using nil as a destination io.Writer since we will Reset() it in the
	// loop below.
	fw := wsflate.NewWriter(nil, func(w io.Writer) wsflate.Compressor {
		// As flat.NewWriter() docs says:
		//   If level is in the range [-2, 9] then the error returned will
		//   be nil.
		f, _ := flate.NewWriter(w, 9)
		return f
	})
	// Using nil as a source io.Reader since we will Reset() it in the loop
	// below.
	fr := wsflate.NewReader(nil, func(r io.Reader) wsflate.Decompressor {
		return flate.NewReader(r)
	})

	// MessageState implements wsutil.Extension and is used to check whether
	// received WebSocket message is compressed. That is, it's generally
	// possible to receive uncompressed messaged even if compression extension
	// was negotiated.
	var msg wsflate.MessageState

	// Note that control frames are all written without compression.
	controlHandler := wsutil.ControlFrameHandler(conn, ws.StateServerSide)
	rd := wsutil.Reader{
		Source:         conn,
		State:          ws.StateServerSide | ws.StateExtended,
		CheckUTF8:      false,
		OnIntermediate: controlHandler,
		Extensions:     []wsutil.RecvExtension{&msg},
	}

	wr := wsutil.NewWriter(conn, ws.StateServerSide|ws.StateExtended, 0)
	wr.SetExtensions(&msg)

	for {
		h, err := rd.NextFrame()
		if err != nil {
			log.Printf("next frame error: %v", err)
			return
		}
		if h.OpCode.IsControl() {
			if err := controlHandler(h, &rd); err != nil {
				log.Printf("handle control frame error: %v", err)
				return
			}
			continue
		}

		wr.ResetOp(h.OpCode)

		var (
			src io.Reader = &rd
			dst io.Writer = wr
		)
		if msg.IsCompressed() {
			fr.Reset(src)
			fw.Reset(dst)
			src = fr
			dst = fw
		}
		// Copy incoming bytes right into writer, probably through decompressor
		// and compressor.
		if _, err = io.Copy(dst, src); err != nil {
			log.Fatal(err)
		}
		if msg.IsCompressed() {
			// Flush the flate writer.
			if err = fw.Close(); err != nil {
				log.Fatal(err)
			}
		}
		// Flush WebSocket fragment writer. We could send multiple fragments
		// for large messages.
		if err = wr.Flush(); err != nil {
			log.Fatal(err)
		}
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	u := ws.HTTPUpgrader{
		Extension: func(opt httphead.Option) bool {
			log.Printf("extension: %s", opt)
			return false
		},
	}
	conn, _, _, err := u.Upgrade(r, w)
	if err != nil {
		log.Printf("upgrade error: %s", err)
		return
	}
	defer conn.Close()

	state := ws.StateServerSide

	textPending := false
	utf8Reader := wsutil.NewUTF8Reader(nil)
	cipherReader := wsutil.NewCipherReader(nil, [4]byte{0, 0, 0, 0})

	for {
		header, err := ws.ReadHeader(conn)
		if err != nil {
			log.Printf("read header error: %s", err)
			break
		}
		if err = ws.CheckHeader(header, state); err != nil {
			log.Printf("header check error: %s", err)
			conn.Write(closeProtocolError)
			return
		}

		cipherReader.Reset(
			io.LimitReader(conn, header.Length),
			header.Mask,
		)

		var utf8Fin bool
		var r io.Reader = cipherReader

		switch header.OpCode {
		case ws.OpPing:
			header.OpCode = ws.OpPong
			header.Masked = false
			ws.WriteHeader(conn, header)
			io.CopyN(conn, cipherReader, header.Length)
			continue

		case ws.OpPong:
			io.CopyN(ioutil.Discard, conn, header.Length)
			continue

		case ws.OpClose:
			utf8Fin = true

		case ws.OpContinuation:
			if textPending {
				utf8Reader.Source = cipherReader
				r = utf8Reader
			}
			if header.Fin {
				state = state.Clear(ws.StateFragmented)
				textPending = false
				utf8Fin = true
			}

		case ws.OpText:
			utf8Reader.Reset(cipherReader)
			r = utf8Reader

			if !header.Fin {
				state = state.Set(ws.StateFragmented)
				textPending = true
			} else {
				utf8Fin = true
			}

		case ws.OpBinary:
			if !header.Fin {
				state = state.Set(ws.StateFragmented)
			}
		}

		payload := make([]byte, header.Length)
		_, err = io.ReadFull(r, payload)
		if err == nil && utf8Fin && !utf8Reader.Valid() {
			err = wsutil.ErrInvalidUTF8
		}
		if err != nil {
			log.Printf("read payload error: %s", err)
			if err == wsutil.ErrInvalidUTF8 {
				conn.Write(closeInvalidPayload)
			} else {
				conn.Write(ws.CompiledClose)
			}
			return
		}

		if header.OpCode == ws.OpClose {
			code, reason := ws.ParseCloseFrameData(payload)
			log.Printf("close frame received: %v %v", code, reason)

			if !code.Empty() {
				switch {
				case code.IsProtocolSpec() && !code.IsProtocolDefined():
					err = fmt.Errorf("close code from spec range is not defined")
				default:
					err = ws.CheckCloseFrameData(code, reason)
				}
				if err != nil {
					log.Printf("invalid close data: %s", err)
					conn.Write(closeProtocolError)
				} else {
					ws.WriteFrame(conn, ws.NewCloseFrame(ws.NewCloseFrameBody(
						code, "",
					)))
				}
				return
			}

			conn.Write(ws.CompiledClose)
			return
		}

		header.Masked = false
		ws.WriteHeader(conn, header)
		conn.Write(payload)
	}
}
