package ws

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"hash"
	"math/rand"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
)

const (
	nonceSize  = 24
	acceptSize = 28
	shaSumSize = 20
)

var (
	headerUpgrade       = textproto.CanonicalMIMEHeaderKey("Upgrade")
	headerConnection    = textproto.CanonicalMIMEHeaderKey("Connection")
	headerHost          = textproto.CanonicalMIMEHeaderKey("Host")
	headerOrigin        = textproto.CanonicalMIMEHeaderKey("Origin")
	headerSecVersion    = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Version")
	headerSecProtocol   = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Protocol")
	headerSecExtensions = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Extensions")
	headerSecKey        = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Key")
	headerSecAccept     = textproto.CanonicalMIMEHeaderKey("Sec-Websocket-Accept")
)

var ErrBadNonce = fmt.Errorf("nonce size is not %d", nonceSize)

var WebSocketMagic = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

type request struct {
	http.Request
	Nonce      [nonceSize]byte
	Protocols  []string
	Extensions []string
}

func (req *request) Reset(urlstr string, headers http.Header, protocols, extensions []string) error {
	u, err := url.ParseRequestURI(urlstr)
	if err != nil {
		return err
	}

	req.URL = u

	newNonce(req.Nonce[:])
	req.Header.Set(headerSecKey, string(req.Nonce[:]))

	req.Protocols = protocols
	if protocols != nil {
		req.Header.Set(headerSecProtocol, strings.Join(protocols, ", "))
	}

	req.Extensions = extensions
	if extensions != nil {
		req.Header.Set(headerSecExtensions, strings.Join(extensions, ", "))
	}

	req.Header.Set("User-Agent", "") // Disable default user-agent header.

	if headers != nil {
		for k, v := range headers {
			req.Header[k] = v
		}
	}

	return nil
}

var requestPool sync.Pool

func getRequest() *request {
	if req := requestPool.Get(); req != nil {
		return req.(*request)
	}
	return newCommonRequest()
}

func putRequest(req *request) {
	req.URL = nil
	req.Protocols = nil
	req.Extensions = nil

	for k := range req.Header {
		switch k {
		case headerUpgrade, headerConnection, headerSecVersion:
			// leave common headers
		default:
			delete(req.Header, k)
		}
	}

	requestPool.Put(req)
}

func newCommonRequest() *request {
	req := &request{
		Request: http.Request{
			Header: make(http.Header),
		},
	}

	req.Header.Set(headerUpgrade, "websocket")
	req.Header.Set(headerConnection, "Upgrade")
	req.Header.Set(headerSecVersion, "13")

	return req
}

var sha1Pool sync.Pool

func acquireSha1() hash.Hash {
	if h := sha1Pool.Get(); h != nil {
		return h.(hash.Hash)
	}
	return sha1.New()
}

func releaseSha1(h hash.Hash) {
	h.Reset()
	sha1Pool.Put(h)
}

// todo bench put expect to req as array
func checkNonce(accept string, nonce [nonceSize]byte) bool {
	if len(accept) != 28 {
		return false
	}

	expect := makeAccept(nonce[:])

	return string(expect) == accept
}

//const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
//
//func newNonce(dest []byte) {
//	for i := 0; i < 22; i++ {
//		dest[i] = alphabet[rand.Intn(len(alphabet))]
//	}
//	dest[22] = '='
//	dest[23] = '='
//}

func randBytes(n int) []byte {
	bts := make([]byte, n)
	if _, err := rand.Read(bts); err != nil {
		panic(fmt.Sprintf("rand read error: %s", err))
	}
	return bts
}

func newNonce(dest []byte) {
	base64.StdEncoding.Encode(dest, randBytes(16))
}

func makeAccept(nonce []byte) []byte {
	bts := make([]byte, 0, acceptSize+shaSumSize)
	n := putAccept(nonce, bts)
	return bts[:n]
}

func putAccept(nonce, buf []byte) int {
	if cap(buf) < acceptSize+shaSumSize {
		panic(fmt.Sprintf("buffer cap is %d; want at least %d", len(buf), acceptSize+shaSumSize))
	}
	if len(nonce) != nonceSize {
		panic(fmt.Sprintf("nonce size is %d; want %d", len(nonce), nonceSize))
	}

	sha := acquireSha1()
	defer releaseSha1(sha)

	sha.Write(nonce)
	sha.Write(WebSocketMagic)

	buf = buf[:acceptSize]
	sum := sha.Sum(buf[acceptSize:])

	base64.StdEncoding.Encode(buf, sum)

	return acceptSize
}
