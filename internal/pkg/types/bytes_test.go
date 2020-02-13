package types

import (
	"bytes"
	"testing"

	"github.com/filecoin-project/go-filecoin/internal/pkg/encoding"
	tf "github.com/filecoin-project/go-filecoin/internal/pkg/testhelpers/testflags"
	"github.com/stretchr/testify/assert"
)

func TestRoundtrip(t *testing.T) {
	tf.UnitTest(t)

	cases := [][]byte{nil, {}, []byte("bytes")}
	for _, c := range cases {
		raw, err := encoding.Encode(c)
		assert.NoError(t, err)
		var out []byte
		err = encoding.Decode(raw, &out)
		assert.NoError(t, err)
		switch {
		case c == nil:
			assert.Nil(t, out)
		default:
			assert.True(t, bytes.Equal(c, out))
		}
	}
}
