package commands

import (
	"fmt"
	"testing"

	"github.com/filecoin-project/go-filecoin/fixtures"
	th "github.com/filecoin-project/go-filecoin/testhelpers"
	"github.com/stretchr/testify/assert"
)

func TestOrderbookAsks(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	d := th.NewDaemon(t, th.WithMiner(fixtures.TestMiners[0]), th.KeyFile(fixtures.KeyFilePaths()[0])).Start()
	defer d.ShutdownSuccess()

	addr := fixtures.TestAddresses[0]
	minerAddr := fixtures.TestMiners[0]

	for i := 0; i < 10; i++ {
		d.RunSuccess(
			"miner", "add-ask",
			"--from", addr,
			minerAddr, "1", fmt.Sprintf("%d", i),
		)
	}

	d.RunSuccess("mining", "once")

	list := d.RunSuccess("orderbook", "asks").ReadStdout()
	for i := 0; i < 10; i++ {
		assert.Contains(list, fmt.Sprintf("\"price\":\"%d\"", i))
	}
}