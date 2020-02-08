package actor_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
	. "github.com/filecoin-project/go-filecoin/internal/pkg/vm/actor"

	tf "github.com/filecoin-project/go-filecoin/internal/pkg/testhelpers/testflags"
)

func TestActorFormat(t *testing.T) {
	tf.UnitTest(t)

	accountActor := NewActor(types.AccountActorCodeCid, types.NewAttoFILFromFIL(5))

	formatted := fmt.Sprintf("%v", accountActor)
	assert.Contains(t, formatted, "AccountActor")
	assert.Contains(t, formatted, "balance: 5")
	assert.Contains(t, formatted, "nonce: 0")

	minerActor := NewActor(types.MinerActorCodeCid, types.NewAttoFILFromFIL(5))
	formatted = fmt.Sprintf("%v", minerActor)
	assert.Contains(t, formatted, "MinerActor")

	storageMarketActor := NewActor(types.StorageMarketActorCodeCid, types.NewAttoFILFromFIL(5))
	formatted = fmt.Sprintf("%v", storageMarketActor)
	assert.Contains(t, formatted, "StorageMarketActor")
}
