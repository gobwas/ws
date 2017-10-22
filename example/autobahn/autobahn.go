package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const dir = "./example/autobahn"

var (
	addr    = flag.String("listen", ":9001", "addr to listen")
	reports = flag.String("reports", dir+"/reports", "path to reports directory")
	static  = flag.String("static", dir+"/static", "path to static assets directory")
)

func main() {
	flag.Parse()

	log.Printf("reports dir is set to: %s", *reports)
	log.Printf("static dir is set to: %s", *static)

	http.HandleFunc("/", handlerIndex())
	http.HandleFunc("/ws", wsHandler())
	http.HandleFunc("/wsutil", wsutilHandler())
	http.HandleFunc("/helpers/low", helpersLowLevelHandler())
	http.HandleFunc("/helpers/high", helpersHighLevelHandler())
	http.Handle("/reports/", http.StripPrefix("/reports/", http.FileServer(http.Dir(*reports))))

	log.Printf("ready to listen on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

var (
	closeInvalidPayload = ws.MustCompileFrame(
		ws.NewCloseFrame(ws.StatusInvalidFramePayloadData, ""),
	)
	closeProtocolError = ws.MustCompileFrame(
		ws.NewCloseFrame(ws.StatusProtocolError, ""),
	)
)

func helpersHighLevelHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
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
}

func helpersLowLevelHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
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
					err := wsutil.HandleClientControl(conn, m.OpCode, m.Payload)
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
}

func wsutilHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(resp http.ResponseWriter, req *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(req, resp, nil)
		if err != nil {
			log.Printf("upgrade error: %s", err)
			return
		}
		defer conn.Close()

		state := ws.StateServerSide

		ch := wsutil.ControlHandler(conn, state)
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
}

func wsHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
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
						ws.WriteFrame(conn, ws.NewCloseFrame(code, ""))
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
}

func handlerIndex() func(w http.ResponseWriter, r *http.Request) {
	index, err := os.Open(*static + "/index.html")
	if err != nil {
		log.Fatal(err)
	}
	bts, err := ioutil.ReadAll(index)
	if err != nil {
		log.Fatal(err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("reqeust to %s", r.URL)
		switch r.URL.Path {
		case "/":
			buf := bytes.NewBuffer(bts)
			_, err := buf.WriteTo(w)
			if err != nil {
				log.Printf("write index bytes error: %s", err)
			}
		case "/favicon.ico":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}
