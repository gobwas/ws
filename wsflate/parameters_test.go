package wsflate

import (
	"testing"

	"github.com/gobwas/httphead"
)

func TestParametersParseDuplicate(t *testing.T) {
	for _, header := range []string{
		"permessage-deflate; client_max_window_bits; client_max_window_bits",
		"permessage-deflate; client_max_window_bits; client_max_window_bits=10",
		"permessage-deflate; client_max_window_bits=10; client_max_window_bits",
		"permessage-deflate; client_max_window_bits=10; client_max_window_bits=10",
		"permessage-deflate; server_max_window_bits=10; server_max_window_bits=10",
		"permessage-deflate; client_no_context_takeover; client_no_context_takeover",
		"permessage-deflate; server_no_context_takeover; server_no_context_takeover",
	} {
		t.Run(header, func(t *testing.T) {
			opts, ok := httphead.ParseOptions([]byte(header), nil)
			if !ok || len(opts) != 1 {
				t.Fatalf(
					"can't parse test header: %q",
					header,
				)
			}
			var p Parameters
			if err := p.Parse(opts[0]); err == nil {
				t.Errorf(
					"Parse() = %+v, nil; want duplicate parameter error",
					p,
				)
			}
		})
	}
}
