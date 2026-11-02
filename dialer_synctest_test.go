//go:build go1.25

package ws

import (
	"context"
	"fmt"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

// TestDialerTimeoutBoundsHandshake checks that Dialer.Timeout bounds the whole
// Dial(), the handshake included, also when a context with a later deadline
// is given. The connection takes the handshake request and never answers it;
// the only way out is a deadline.
func TestDialerTimeoutBoundsHandshake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			expired = make(chan error, 10)
			timer   *time.Timer
		)
		conn := stubConn{
			write: func(p []byte) (int, error) {
				return len(p), nil
			},
			read: func(p []byte) (int, error) {
				return 0, <-expired
			},
			setDeadline: func(d time.Time) error {
				if timer != nil {
					timer.Stop()
				}
				if d.IsZero() {
					return nil
				}
				fire := func() {
					expired <- errTimeout{fmt.Errorf("stub: i/o timeout")}
				}
				if wait := time.Until(d); wait <= 0 {
					fire()
				} else {
					timer = time.AfterFunc(wait, fire)
				}
				return nil
			},
		}
		d := Dialer{
			Timeout: 100 * time.Millisecond,
			NetDial: func(context.Context, string, string) (net.Conn, error) {
				return conn, nil
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		start := time.Now()
		_, _, _, err := d.Dial(ctx, "ws://example.com")
		took := time.Since(start)
		if err != context.DeadlineExceeded {
			t.Fatalf(
				"unexpected error: %v; want %v",
				err, context.DeadlineExceeded,
			)
		}
		if took != d.Timeout {
			t.Fatalf(
				"Dial() took %s; want exactly Timeout %s",
				took, d.Timeout,
			)
		}
	})
}
