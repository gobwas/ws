package ws

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"unsafe"

	"github.com/gobwas/httphead"
)

const (
	// RFC6455: The value of this header field MUST be a nonce consisting of a
	// randomly selected 16-byte value that has been base64-encoded (see
	// Section 4 of [RFC4648]).  The nonce MUST be selected randomly for each
	// connection.
	nonceKeySize = 16
	nonceSize    = 24 // base64.StdEncoding.EncodedLen(nonceKeySize)

	// RFC6455: The value of this header field is constructed by concatenating
	// /key/, defined above in step 4 in Section 4.2.2, with the string
	// "258EAFA5- E914-47DA-95CA-C5AB0DC85B11", taking the SHA-1 hash of this
	// concatenated value to obtain a 20-byte value and base64- encoding (see
	// Section 4 of [RFC4648]) this 20-byte hash.
	acceptSize = 28 // base64.StdEncoding.EncodedLen(sha1.Size)
)

var ErrBadNonce = fmt.Errorf("nonce size is not %d", nonceSize)

var WebSocketMagic = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

type request struct {
	http.Request
	Nonce      [nonceSize]byte
	Protocols  []string
	Extensions []httphead.Option
}

func (req *request) Reset(urlstr string, headers http.Header, protocols []string, extensions []httphead.Option) error {
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
		buf := bytes.Buffer{}
		httphead.WriteOptions(&buf, extensions)
		req.Header.Set(headerSecExtensions, buf.String())
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
	if len(accept) != acceptSize {
		return false
	}

	var expect [acceptSize]byte
	putAccept(nonce, expect[:])

	return btsToString(expect[:]) == accept
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
	base64.StdEncoding.Encode(dest, randBytes(nonceKeySize))
}

// putAccept generates accept bytes and puts them into p.
// Given buffer should be exactly acceptSize bytes. If not putAccept will panic.
func putAccept(nonce [nonceSize]byte, p []byte) {
	if len(p) != acceptSize {
		panic(fmt.Sprintf("buffer size is %d; want %d", len(p), acceptSize))
	}

	sha := acquireSha1()
	defer releaseSha1(sha)

	var sb [sha1.Size]byte
	sh := uintptr(unsafe.Pointer(&sb))
	sum := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{Data: sh, Len: 0, Cap: sha1.Size}))

	nh := uintptr(unsafe.Pointer(&nonce))
	nb := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{Data: nh, Len: nonceSize, Cap: nonceSize}))

	sha.Write(nb)
	sha.Write(WebSocketMagic)
	sum = sha.Sum(sum)

	base64.StdEncoding.Encode(p, sum)
}

func writeAccept(w io.Writer, nonce [nonceSize]byte) (int, error) {
	var b [acceptSize]byte
	bp := uintptr(unsafe.Pointer(&b))
	bts := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{Data: bp, Len: acceptSize, Cap: acceptSize}))

	putAccept(nonce, bts)

	return w.Write(bts)
}
