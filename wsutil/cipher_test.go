package wsutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/gobwas/ws"
)

func TestCipherReader(t *testing.T) {
	for i, test := range []struct {
		label string
		data  []byte
		chop  int
	}{
		{
			label: "simple",
			data:  []byte("hello, websockets!"),
			chop:  512,
		},
		{
			label: "chopped",
			data:  []byte("hello, websockets!"),
			chop:  3,
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			mask := ws.NewMask()
			masked := make([]byte, len(test.data))
			copy(masked, test.data)
			ws.Cipher(masked, mask, 0)

			src := &chopReader{bytes.NewReader(masked), test.chop}
			rd := NewCipherReader(src, mask)

			bts, err := ioutil.ReadAll(rd)
			if err != nil {
				t.Errorf("unexpected error: %s", err)
				return
			}
			if !reflect.DeepEqual(bts, test.data) {
				t.Errorf("read data is not equal:\n\tact:\t%#v\n\texp:\t%#x\n", bts, test.data)
				return
			}
		})
	}
}
