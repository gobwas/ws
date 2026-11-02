// Package saferio provides I/O functions that avoid allocating large amounts
// of memory unnecessarily. This is intended for reading data from a network
// peer where the size is part of the input data but the peer may be lying
// about it, as a WebSocket frame header does with the payload length.
//
// It mirrors the internal/saferio package of the standard library, which is
// used by encoding/gob, debug/elf and the like for the same reason.
package saferio

import (
	"io"
)

// ReadN reads n bytes from r, but avoids allocating all n bytes before they
// arrive if n is larger than lim. This avoids crashing the program by
// allocating all n bytes in cases where n is incorrect or malicious.
//
// The buffer starts at most lim bytes long and doubles as bytes actually
// arrive, never exceeding n, so a peer can not make the caller allocate more
// than it has actually sent (plus the growth slack), and the returned slice
// has exactly n bytes of capacity.
//
// It panics if lim is not positive, since trusting nothing is not a sane
// configuration and is always a bug at the call site.
//
// The error is io.EOF only if no bytes were read. If an io.EOF happens after
// reading some but not all the bytes, ReadN returns io.ErrUnexpectedEOF.
//
// Unlike the standard library version, which allocates up to 10 MiB without
// concern and grows by append() above that, ReadN takes the limit as an
// argument and keeps the final capacity exact, which is what the callers
// measured for.
//
// It panics if the given limit is <=0.
//
// NOTE: io.ReadAll(io.LimitReader(r, n)) is not used here because it ignores
// n: it starts from a 512-byte buffer and grows it by the append() policy,
// which costs several times more allocations even for small payloads, and it
// treats a truncated payload as a clean EOF.
func ReadN(r io.Reader, lim, n int64) ([]byte, error) {
	if lim <= 0 {
		panic("saferio: ReadN() limit must be greater than zero")
	}
	c := n
	if c > lim {
		c = lim
	}
	p := make([]byte, 0, c)
	for {
		// NOTE: cap(p) never exceeds n, so reading up to the capacity never
		// reads past the payload.
		m, err := io.ReadFull(r, p[len(p):cap(p)])
		p = p[:len(p)+m]
		if err != nil {
			if err == io.EOF && len(p) > 0 {
				err = io.ErrUnexpectedEOF
			}
			return p, err
		}
		if int64(len(p)) == n {
			return p, nil
		}
		c = 2 * int64(cap(p))
		if c > n {
			c = n
		}
		q := make([]byte, len(p), c)
		copy(q, p)
		p = q
	}
}
