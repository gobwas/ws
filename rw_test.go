package ws

import (
	"fmt"
	"strings"
)

type RWTestCase struct {
	Data   []byte
	Header Header
	Err    bool
}

type RWBenchCase struct {
	label  string
	header Header
}

var RWBenchCases = []RWBenchCase{
	{
		"no-mask",
		Header{
			OpCode: OpText,
			Fin:    true,
		},
	},
	{
		"mask",
		Header{
			OpCode: OpText,
			Fin:    true,
			Masked: true,
			Mask:   NewMask(),
		},
	},
	{
		"mask-u16",
		Header{
			OpCode: OpText,
			Fin:    true,
			Length: len16,
			Masked: true,
			Mask:   NewMask(),
		},
	},
	{
		"mask-u64",
		Header{
			OpCode: OpText,
			Fin:    true,
			Length: len64,
			Masked: true,
			Mask:   NewMask(),
		},
	},
}

var RWTestCases = []RWTestCase{
	{
		Data: bits("1 001 0001 0 1100100"),
		//          _ ___ ____ _ _______
		//          |  |   |   |    |
		//         Fin |   |  Mask Length
		//            Rsv  |
		//             TextFrame
		Header: Header{
			Fin:    true,
			Rsv:    Rsv(false, false, true),
			OpCode: OpText,
			Length: 100,
		},
	},
	{
		Data: bits("1 001 0001 1 1100100 00000001 10001000 00000000 11111111"),
		//          _ ___ ____ _ _______ ___________________________________
		//          |  |   |   |    |                     |
		//         Fin |   |  Mask Length             Mask value
		//            Rsv  |
		//             TextFrame
		Header: Header{
			Fin:    true,
			Rsv:    Rsv(false, false, true),
			OpCode: OpText,
			Length: 100,
			Masked: true,
			Mask:   [4]byte{0x01, 0x88, 0x00, 0xff},
		},
	},
	{
		Data: bits("0 110 0010 0 1111110 00001111 11111111"),
		//          _ ___ ____ _ _______ _________________
		//          |  |   |   |    |            |
		//         Fin |   |  Mask Length   Length value
		//            Rsv  |
		//             BinaryFrame
		Header: Header{
			Fin:    false,
			Rsv:    Rsv(true, true, false),
			OpCode: OpBinary,
			Length: 0x0fff,
		},
	},
	{
		Data: bits("1 000 1010 0 1111111 01111111 00000000 00000000 00000000 00000000 00000000 00000000 00000000"),
		//          _ ___ ____ _ _______ _______________________________________________________________________
		//          |  |   |   |    |                                       |
		//         Fin |   |  Mask Length                              Length value
		//            Rsv  |
		//              PongFrame
		Header: Header{
			Fin:    true,
			Rsv:    Rsv(false, false, false),
			OpCode: OpPong,
			Length: 0x7f00000000000000,
		},
	},
}

func bits(s string) []byte {
	s = strings.ReplaceAll(s, " ", "")
	bts := make([]byte, len(s)/8)

	for i, j := 0, 0; i < len(s); i, j = i+8, j+1 {
		fmt.Sscanf(s[i:], "%08b", &bts[j])
	}

	return bts
}
