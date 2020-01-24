package consensus_test

import (
	"context"
	"testing"

	"github.com/filecoin-project/go-address"
	bstore "github.com/ipfs/go-ipfs-blockstore"

	"github.com/filecoin-project/go-filecoin/internal/pkg/block"
	"github.com/filecoin-project/go-filecoin/internal/pkg/cborutil"
	"github.com/filecoin-project/go-filecoin/internal/pkg/consensus"
	"github.com/filecoin-project/go-filecoin/internal/pkg/repo"
	tf "github.com/filecoin-project/go-filecoin/internal/pkg/testhelpers/testflags"
	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/state"
	gengen "github.com/filecoin-project/go-filecoin/tools/gengen/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTotal(t *testing.T) {
	tf.UnitTest(t)
	t.Skip("using legacy vm")

	ctx := context.Background()

	numCommittedSectors := uint64(19)
	cst, bs, _, st := requireMinerWithNumCommittedSectors(ctx, t, numCommittedSectors)

	as := consensus.NewActorStateStore(nil, cst, bs, consensus.NewDefaultProcessor())
	snapshot := as.StateTreeSnapshot(st, types.NewBlockHeight(0))

	actual, err := consensus.NewPowerTableView(snapshot).Total(ctx)
	require.NoError(t, err)

	expected := types.NewBytesAmount(types.OneKiBSectorSize.Uint64() * numCommittedSectors)

	assert.True(t, expected.Equal(actual))
}

func TestMiner(t *testing.T) {
	tf.UnitTest(t)
	t.Skip("using legacy vm")

	ctx := context.Background()

	numCommittedSectors := uint64(12)
	cst, bs, addr, st := requireMinerWithNumCommittedSectors(ctx, t, numCommittedSectors)

	as := consensus.NewActorStateStore(nil, cst, bs, consensus.NewDefaultProcessor())
	snapshot := as.StateTreeSnapshot(st, types.NewBlockHeight(0))

	actual, err := consensus.NewPowerTableView(snapshot).Miner(ctx, addr)
	require.NoError(t, err)

	expected := types.NewBytesAmount(types.OneKiBSectorSize.Uint64() * numCommittedSectors)

	assert.Equal(t, expected, actual)
}

func requireMinerWithNumCommittedSectors(ctx context.Context, t *testing.T, numCommittedSectors uint64) (*cborutil.IpldStore, bstore.Blockstore, address.Address, state.Tree) {
	r := repo.NewInMemoryRepo()
	bs := bstore.NewBlockstore(r.Datastore())
	cst := cborutil.NewIpldStore(bs)

	// set up genesis block containing some miners with non-zero power
	genCfg := &gengen.GenesisCfg{
		ProofsMode: types.TestProofsMode,
		Keys:       1,
		Miners: []*gengen.CreateStorageMinerConfig{
			{
				NumCommittedSectors: numCommittedSectors,
				SectorSize:          types.OneKiBSectorSize.Uint64(),
			},
		},
		Network: "ptvtest",
	}

	info, err := gengen.GenGen(ctx, genCfg, cst, bs)
	require.NoError(t, err)

	var calcGenBlk block.Block
	require.NoError(t, cst.Get(ctx, info.GenesisCid, &calcGenBlk))

	stateTree, err := state.NewTreeLoader().LoadStateTree(ctx, cst, calcGenBlk.StateRoot.Cid)
	require.NoError(t, err)

	return cst, bs, info.Miners[0].Address, stateTree
}
