package storageminerconnector

import (
	"context"
	"errors"
	"math"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-storage-miner"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-filecoin/internal/app/go-filecoin/plumbing/cst"
	"github.com/filecoin-project/go-filecoin/internal/app/go-filecoin/plumbing/msg"
	"github.com/filecoin-project/go-filecoin/internal/pkg/block"
	"github.com/filecoin-project/go-filecoin/internal/pkg/chain"
	"github.com/filecoin-project/go-filecoin/internal/pkg/chainsampler"
	"github.com/filecoin-project/go-filecoin/internal/pkg/consensus"
	"github.com/filecoin-project/go-filecoin/internal/pkg/encoding"
	"github.com/filecoin-project/go-filecoin/internal/pkg/message"
	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/abi"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/actor/builtin/storagemarket"
	vmaddr "github.com/filecoin-project/go-filecoin/internal/pkg/vm/address"
	"github.com/filecoin-project/go-filecoin/internal/pkg/wallet"
)

// StorageMinerNodeConnector is a struct which satisfies the go-storage-miner
// needs of "the node," e.g. interacting with the blockchain, persisting sector
// states to disk, and so forth.
type StorageMinerNodeConnector struct {
	minerAddr  address.Address
	workerAddr address.Address

	chainHeightScheduler *chainsampler.HeightThresholdScheduler

	chainStore *chain.Store
	chainState *cst.ChainStateReadWriter
	outbox     *message.Outbox
	waiter     *msg.Waiter
	wallet     *wallet.Wallet
}

var _ storage.NodeAPI = new(StorageMinerNodeConnector)

// NewStorageMinerNodeConnector produces a StorageMinerNodeConnector, which adapts
// types in this codebase to the interface representing "the node" which is
// expected by the go-storage-miner project.
func NewStorageMinerNodeConnector(minerAddress address.Address, workerAddress address.Address, chainStore *chain.Store, chainState *cst.ChainStateReadWriter, outbox *message.Outbox, waiter *msg.Waiter, wallet *wallet.Wallet) *StorageMinerNodeConnector {
	return &StorageMinerNodeConnector{
		minerAddr:            minerAddress,
		workerAddr:           workerAddress,
		chainHeightScheduler: chainsampler.NewHeightThresholdScheduler(chainStore),
		chainStore:           chainStore,
		chainState:           chainState,
		outbox:               outbox,
		waiter:               waiter,
		wallet:               wallet,
	}
}

// StartHeightListener starts the scheduler that manages height listeners.
func (m *StorageMinerNodeConnector) StartHeightListener(ctx context.Context, htc <-chan interface{}) {
	m.chainHeightScheduler.StartHeightListener(ctx, htc)
}

// StopHeightListener stops the scheduler that manages height listeners.
func (m *StorageMinerNodeConnector) StopHeightListener() {
	m.chainHeightScheduler.Stop()
}

// CancelHeightListener removes on

func (m *StorageMinerNodeConnector) handleNewTipSet(ctx context.Context, previousHead block.TipSet) (block.TipSet, error) {

	return m.chainHeightScheduler.HandleNewTipSet(ctx, previousHead)
}

// SendSelfDeals creates self-deals and sends them to the network.
func (m *StorageMinerNodeConnector) SendSelfDeals(ctx context.Context, pieces ...storage.PieceInfo) (cid.Cid, error) {
	proposals := make([]types.StorageDealProposal, len(pieces))
	for i, piece := range pieces {
		proposals[i] = types.StorageDealProposal{
			PieceRef:             piece.CommP[:],
			PieceSize:            types.Uint64(piece.Size),
			Client:               m.workerAddr,
			Provider:             m.minerAddr,
			ProposalExpiration:   math.MaxUint64,
			Duration:             math.MaxUint64 / 2, // /2 because overflows
			StoragePricePerEpoch: 0,
			StorageCollateral:    0,
			ProposerSignature:    nil,
		}

		proposalBytes, err := encoding.Encode(proposals[i])
		if err != nil {
			return cid.Undef, err
		}

		sig, err := m.wallet.SignBytes(proposalBytes, m.workerAddr)
		if err != nil {
			return cid.Undef, err
		}

		proposals[i].ProposerSignature = &sig
	}

	dealParams, err := abi.ToEncodedValues(proposals)
	if err != nil {
		return cid.Undef, err
	}

	mcid, cerr, err := m.outbox.Send(
		ctx,
		m.workerAddr,
		vmaddr.StorageMarketAddress,
		types.ZeroAttoFIL,
		types.NewGasPrice(1),
		types.NewGasUnits(300),
		true,
		storagemarket.PublishStorageDeals,
		dealParams,
	)
	if err != nil {
		return cid.Undef, err
	}

	err = <-cerr
	if err != nil {
		return cid.Undef, err
	}

	return mcid, nil
}

// WaitForSelfDeals blocks until the provided storage deal-publishing message is
// mined into a block, producing a slice of deal IDs and an exit code when it is
// mined into a block (or an error, if encountered).
func (m *StorageMinerNodeConnector) WaitForSelfDeals(ctx context.Context, mcid cid.Cid) ([]uint64, uint8, error) {
	receiptChan := make(chan *types.MessageReceipt)
	errChan := make(chan error)

	go func() {
		err := m.waiter.Wait(ctx, mcid, func(b *block.Block, message *types.SignedMessage, r *types.MessageReceipt) error {
			receiptChan <- r
			return nil
		})
		if err != nil {
			errChan <- err
		}
	}()

	select {
	case receipt := <-receiptChan:
		if receipt.ExitCode != 0 {
			return nil, receipt.ExitCode, nil
		}

		dealIDValues, err := abi.Deserialize(receipt.Return[0], abi.UintArray)
		if err != nil {
			return nil, 0, err
		}

		dealIds, ok := dealIDValues.Val.([]uint64)
		if !ok {
			return nil, 0, errors.New("decoded deal ids are not a []uint64")
		}

		return dealIds, 0, nil
	case err := <-errChan:
		return nil, 0, err
	case <-ctx.Done():
		return nil, 0, errors.New("context ended prematurely")
	}
}

// SendPreCommitSector creates a pre-commit sector message and sends it to the
// network.
func (m *StorageMinerNodeConnector) SendPreCommitSector(ctx context.Context, sectorID uint64, commR []byte, ticket storage.SealTicket, pieces ...storage.Piece) (cid.Cid, error) {
	dealIds := make([]types.Uint64, len(pieces))
	for i, piece := range pieces {
		dealIds[i] = types.Uint64(piece.DealID)
	}

	info := &types.SectorPreCommitInfo{
		SectorNumber: types.Uint64(sectorID),

		CommR:     commR,
		SealEpoch: types.Uint64(ticket.BlockHeight),
		DealIDs:   dealIds,
	}

	precommitParams, err := abi.ToEncodedValues(info)
	if err != nil {
		return cid.Undef, err
	}

	mcid, cerr, err := m.outbox.Send(
		ctx,
		m.workerAddr,
		m.minerAddr,
		types.ZeroAttoFIL,
		types.NewGasPrice(1),
		types.NewGasUnits(300),
		true,
		storagemarket.PreCommitSector,
		precommitParams,
	)
	if err != nil {
		return cid.Undef, err
	}

	err = <-cerr
	if err != nil {
		return cid.Undef, err
	}

	return mcid, nil
}

// WaitForPreCommitSector blocks until the pre-commit sector message is mined
// into a block, returning the block's height and message's exit code (or an
// error if one is encountered).
func (m *StorageMinerNodeConnector) WaitForPreCommitSector(ctx context.Context, mcid cid.Cid) (uint64, uint8, error) {
	return m.waitForMessageHeight(ctx, mcid)
}

// SendProveCommitSector creates a commit sector message and sends it to the
// network.
func (m *StorageMinerNodeConnector) SendProveCommitSector(ctx context.Context, sectorID uint64, proof []byte, deals ...uint64) (cid.Cid, error) {
	dealIds := make([]types.Uint64, len(deals))
	for i, deal := range deals {
		dealIds[i] = types.Uint64(deal)
	}

	info := &types.SectorProveCommitInfo{
		Proof:    proof,
		SectorID: types.Uint64(sectorID),
		DealIDs:  dealIds,
	}

	commitParams, err := abi.ToEncodedValues(info)
	if err != nil {
		return cid.Undef, err
	}

	mcid, cerr, err := m.outbox.Send(
		ctx,
		m.workerAddr,
		m.minerAddr,
		types.ZeroAttoFIL,
		types.NewGasPrice(1),
		types.NewGasUnits(300),
		true,
		storagemarket.CommitSector,
		commitParams,
	)
	if err != nil {
		return cid.Undef, err
	}

	err = <-cerr
	if err != nil {
		return cid.Undef, err
	}

	return mcid, nil
}

// WaitForProveCommitSector blocks until the provided pre-commit message has
// been mined into the chainStore, producing the height of the block in which the
// message was mined (and the message's exit code) or an error if any is
// encountered.
func (m *StorageMinerNodeConnector) WaitForProveCommitSector(ctx context.Context, mcid cid.Cid) (uint64, uint8, error) {
	return m.waitForMessageHeight(ctx, mcid)
}

// GetSealTicket produces the seal ticket used when pre-committing a sector at
// the moment it is called
func (m *StorageMinerNodeConnector) GetSealTicket(ctx context.Context) (storage.SealTicket, error) {
	ts, err := m.chainStore.GetTipSet(m.chainStore.GetHead())
	if err != nil {
		return storage.SealTicket{}, xerrors.Errorf("getting head ts for SealTicket failed: %w", err)
	}

	h, err := ts.Height()
	if err != nil {
		return storage.SealTicket{}, err
	}

	r, err := m.chainState.SampleRandomness(ctx, types.NewBlockHeight(h-consensus.FinalityEpochs))
	if err != nil {
		return storage.SealTicket{}, xerrors.Errorf("getting randomness for SealTicket failed: %w", err)
	}

	return storage.SealTicket{
		BlockHeight: h,
		TicketBytes: r,
	}, nil
}

// GetSealSeed is used to acquire the seal seed for the provided pre-commit
// message, and provides channels to accommodate chainStore re-orgs. The caller is
// responsible for choosing an interval-value, which is a quantity of blocks to
// wait (after the block in which the pre-commit message is mined) before
// computing and sampling a seed.
func (m *StorageMinerNodeConnector) GetSealSeed(ctx context.Context, preCommitMsg cid.Cid, interval uint64) (seed <-chan storage.SealSeed, err <-chan error, invalidated <-chan struct{}, done <-chan struct{}) {
	sc := make(chan storage.SealSeed)
	hc := make(chan block.TipSetKey)
	ec := make(chan error)
	ic := make(chan struct{})
	dc := make(chan struct{})

	go func() {
		h, exitCode, err := m.waitForMessageHeight(ctx, preCommitMsg)
		if err != nil {
			ec <- err
			return
		}

		if exitCode != 0 {
			ec <- xerrors.Errorf("non-zero exit code for pre-commit message %d", exitCode)
			return
		}

		m.chainHeightScheduler.AddListener(h + interval)
	}()

	// translate tipset key to seal seed handler
	go func() {
		for {
			select {
			case key := <-hc:
				ts, err := m.chainState.GetTipSet(key)
				if err != nil {
					ec <- err
					break
				}

				tsHeight, err := ts.Height()
				if err != nil {
					ec <- err
					break
				}

				randomness, err := m.chainState.SampleRandomness(ctx, types.NewBlockHeight(tsHeight))
				if err != nil {
					ec <- err
					break
				}

				sc <- storage.SealSeed{
					BlockHeight: tsHeight,
					TicketBytes: randomness,
				}
			case <-dc:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return sc, ec, ic, dc
}

type heightAndExitCode struct {
	exitCode uint8
	height   uint64
}

func (m *StorageMinerNodeConnector) waitForMessageHeight(ctx context.Context, mcid cid.Cid) (uint64, uint8, error) {
	height := make(chan heightAndExitCode)
	errChan := make(chan error)

	go func() {
		err := m.waiter.Wait(ctx, mcid, func(b *block.Block, message *types.SignedMessage, r *types.MessageReceipt) error {
			height <- heightAndExitCode{
				height:   b.Height,
				exitCode: r.ExitCode,
			}
			return nil
		})
		if err != nil {
			errChan <- err
		}
	}()

	select {
	case h := <-height:
		return h.height, h.exitCode, nil
	case err := <-errChan:
		return 0, 0, err
	case <-ctx.Done():
		return 0, 0, errors.New("context ended prematurely")
	}
}
