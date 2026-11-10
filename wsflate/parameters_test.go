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

func TestParametersParseWindowBits(t *testing.T) {
	for _, test := range []struct {
		header string
		bits   WindowBits
		valid  bool
	}{
		{
			header: "permessage-deflate; server_max_window_bits=8",
			bits:   8,
			valid:  true,
		},
		{
			header: "permessage-deflate; server_max_window_bits=15",
			bits:   15,
			valid:  true,
		},
		{
			header: `permessage-deflate; server_max_window_bits="10"`,
			bits:   10,
			valid:  true,
		},
		{
			header: "permessage-deflate; server_max_window_bits=08",
		},
		{
			header: "permessage-deflate; server_max_window_bits=010",
		},
		{
			header: `permessage-deflate; server_max_window_bits=":"`,
		},
		{
			header: `permessage-deflate; server_max_window_bits="?"`,
		},
		{
			header: "permessage-deflate; server_max_window_bits=7",
		},
		{
			header: "permessage-deflate; server_max_window_bits=16",
		},
	} {
		t.Run(test.header, func(t *testing.T) {
			opts, ok := httphead.ParseOptions([]byte(test.header), nil)
			if !ok || len(opts) != 1 {
				t.Fatalf(
					"can't parse test header: %q",
					test.header,
				)
			}
			var p Parameters
			err := p.Parse(opts[0])
			if test.valid && (err != nil || p.ServerMaxWindowBits != test.bits) {
				t.Errorf(
					"Parse() = %+v, %v; want ServerMaxWindowBits %d, nil",
					p, err, test.bits,
				)
			}
			if !test.valid && err == nil {
				t.Errorf(
					"Parse() = %+v, nil; want invalid parameter error",
					p,
				)
			}
		})
	}
}
