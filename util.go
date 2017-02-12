package ws

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"unsafe"
)

func strToBytes(str string) []byte {
	s := *(*reflect.StringHeader)(unsafe.Pointer(&str))
	b := &reflect.SliceHeader{Data: s.Data, Len: s.Len, Cap: s.Len}
	return *(*[]byte)(unsafe.Pointer(b))
}

func btsToString(bts []byte) string {
	b := *(*reflect.SliceHeader)(unsafe.Pointer(&bts))
	s := &reflect.StringHeader{Data: b.Data, Len: b.Len}
	return *(*string)(unsafe.Pointer(s))
}

func strToNonce(str string) [nonceSize]byte {
	s := *(*reflect.StringHeader)(unsafe.Pointer(&str))
	n := *(*[nonceSize]byte)(unsafe.Pointer(s.Data))
	return n
}

func btsToNonce(bts []byte) [nonceSize]byte {
	b := *(*reflect.SliceHeader)(unsafe.Pointer(&bts))
	n := *(*[nonceSize]byte)(unsafe.Pointer(b.Data))
	return n
}

// asciiToInt converts bytes to int.
func asciiToInt(bts []byte) (ret int, err error) {
	// ASCII numbers all start with the high-order bits 0011.
	// If you see that, and the next bits are 0-9 (0000 - 1001) you can grab those
	// bits and interpret them directly as an integer.
	var n int
	if n = len(bts); n < 1 {
		return 0, fmt.Errorf("converting empty bytes to int")
	}
	for i := 0; i < n; i++ {
		if bts[i]&0xf0 != 0x30 {
			return 0, fmt.Errorf("%s is not a numeric character", string(bts[i]))
		}
		ret += int(bts[i]&0xf) * pow(10, n-i-1)
	}
	return ret, nil
}

// pow for integers implementation.
// See Donald Knuth, The Art of Computer Programming, Volume 2, Section 4.6.3
func pow(a, b int) int {
	p := 1
	for b > 0 {
		if b&1 != 0 {
			p *= a
		}
		b >>= 1
		a *= a
	}
	return p
}

func hostport(u *url.URL) string {
	host, port := split2(u.Host, ':')
	if port != "" {
		return u.Host
	}
	if u.Scheme == "wss" {
		return host + ":443"
	}
	return host + ":80"
}

func split2(s string, sep byte) (a, b string) {
	if i := strings.LastIndexByte(s, sep); i != -1 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func bsplit3(bts []byte, sep byte) (b1, b2, b3 []byte) {
	a := bytes.IndexByte(bts, sep)
	b := bytes.IndexByte(bts[a+1:], sep)
	if a == -1 || b == -1 {
		return bts, nil, nil
	}
	b += a + 1
	return bts[:a], bts[a+1 : b], bts[b+1:]
}

func bsplit2(bts []byte, sep byte) (b1, b2 []byte) {
	if i := bytes.LastIndexByte(bts, sep); i != -1 {
		return bts[:i], bts[i+1:]
	}
	return bts, nil
}

func btrim(bts []byte) []byte {
	var i, j int
	for i = 0; i < len(bts) && (bts[i] == ' ' || bts[i] == '\t'); {
		i++
	}
	for j = len(bts); j > i && (bts[j-1] == ' ' || bts[j-1] == '\t'); {
		j--
	}
	return bts[i:j]
}

func strHasToken(header, token string) bool {
	var pos int
	for i := 0; i <= len(header); i++ {
		if i == len(header) || header[i] == ',' {
			v := strings.TrimSpace(header[pos:i])
			if strEqualFold(v, token) {
				return true
			}
			pos = i + 1
		}
	}
	return false
}

func btsHasToken(header, token []byte) bool {
	var pos int
	for i := 0; i <= len(header); i++ {
		if i == len(header) || header[i] == ',' {
			v := bytes.TrimSpace(header[pos:i])
			if btsEqualFold(v, token) {
				return true
			}
			pos = i + 1
		}
	}
	return false
}

const (
	toLower  = 'a' - 'A'      // for use with OR.
	toUpper  = ^byte(toLower) // for use with AND.
	toLower8 = uint64(toLower) |
		uint64(toLower)<<8 |
		uint64(toLower)<<16 |
		uint64(toLower)<<24 |
		uint64(toLower)<<32 |
		uint64(toLower)<<40 |
		uint64(toLower)<<48 |
		uint64(toLower)<<56
)

// Algorithm below is like standard textproto/CanonicalMIMEHeaderKey, except
// that it operates with slice of bytes and modifies it inplace without copying.
func canonicalizeHeaderKey(k []byte) {
	if len(k) == 0 {
		return
	}
	var i int
	for {
		k[0] &= toUpper

		i = bytes.IndexByte(k, '-')
		if i == -1 {
			break
		}

		for j := 1; j < i; j++ {
			k[j] |= toLower
		}
		k = k[i+1:]
	}

	for j := 1; j < len(k); j++ {
		k[j] |= toLower
	}
}

// readLine is a wrapper around bufio.Reader.ReadLine(), it calls ReadLine()
// until full line will be read.
func readLine(br *bufio.Reader) (line []byte, err error) {
	var more bool
	var bts []byte
	for {
		bts, more, err = br.ReadLine()
		if err != nil {
			return
		}
		// Avoid copying bytes to the nil slice.
		if line == nil {
			line = bts
		} else {
			line = append(line, bts...)
		}
		if !more {
			break
		}
	}
	return
}

// strEqualFold checks s to be case insensitive equal to p.
// Note that p must be only ascii letters. That is, every byte in p belongs to
// range ['a','z'] or ['A','Z'].
func strEqualFold(s, p string) bool {
	if len(s) != len(p) {
		return false
	}

	n := len(s)

	// Prepare manual conversion on bytes that not lay in uint64.
	// We divide here by 16, not by 8 cause it is still faster
	// linear compare for short strings.
	m := n % 16
	for i := 0; i < m; i++ {
		if s[i]|toLower != p[i]|toLower {
			return false
		}
	}

	// Iterate over uint64 parts of s.
	n = (n - m) >> 3
	if n == 0 {
		// There are no bytes to compare.
		return true
	}

	ah := *(*reflect.StringHeader)(unsafe.Pointer(&s))
	ap := ah.Data + uintptr(m)
	bh := *(*reflect.StringHeader)(unsafe.Pointer(&p))
	bp := bh.Data + uintptr(m)

	for i := 0; i < n; i, ap, bp = i+1, ap+8, bp+8 {
		av := *(*uint64)(unsafe.Pointer(ap))
		bv := *(*uint64)(unsafe.Pointer(bp))
		if av|toLower8 != bv|toLower8 {
			return false
		}
	}

	return true
}

// btsEqualFold checks s to be case insensitive equal to p.
// Note that p must be only ascii letters. That is, every byte in p belongs to
// range ['a','z'] or ['A','Z'].
func btsEqualFold(s, p []byte) bool {
	if len(s) != len(p) {
		return false
	}

	n := len(s)

	// Prepare manual conversion on bytes that not lay in uint64.
	// We divide here by 16, not by 8 cause it is still faster
	// linear compare for short strings.
	m := n % 16
	for i := 0; i < m; i++ {
		if s[i]|toLower != p[i]|toLower {
			return false
		}
	}

	// Iterate over uint64 parts of s.
	n = (n - m) >> 3
	if n == 0 {
		// There are no bytes to compare.
		return true
	}

	ah := *(*reflect.SliceHeader)(unsafe.Pointer(&s))
	ap := ah.Data + uintptr(m)
	bh := *(*reflect.SliceHeader)(unsafe.Pointer(&p))
	bp := bh.Data + uintptr(m)

	for i := 0; i < n; i, ap, bp = i+1, ap+8, bp+8 {
		av := *(*uint64)(unsafe.Pointer(ap))
		bv := *(*uint64)(unsafe.Pointer(bp))
		if av|toLower8 != bv|toLower8 {
			return false
		}
	}

	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
