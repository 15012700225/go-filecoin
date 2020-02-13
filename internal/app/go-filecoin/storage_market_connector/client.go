package storagemarketconnector

import (
	"bytes"
	"context"

	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/abi/big"
	"github.com/filecoin-project/specs-actors/actors/builtin/market"
	"github.com/ipfs/go-cid"
	xerrors "github.com/pkg/errors"

	"github.com/filecoin-project/go-address"
	spaminer "github.com/filecoin-project/specs-actors/actors/builtin/miner"
	spapow "github.com/filecoin-project/specs-actors/actors/builtin/power"

	fcabi "github.com/filecoin-project/go-filecoin/internal/pkg/vm/abi"
	fcsm "github.com/filecoin-project/go-filecoin/internal/pkg/vm/actor/builtin/storagemarket"

	"github.com/filecoin-project/go-fil-markets/storagemarket"
	"github.com/ipfs/go-hamt-ipld"
	cbor "github.com/ipfs/go-ipld-cbor"

	"github.com/filecoin-project/go-filecoin/internal/app/go-filecoin/plumbing/msg"
	"github.com/filecoin-project/go-filecoin/internal/pkg/message"
	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
	vmaddr "github.com/filecoin-project/go-filecoin/internal/pkg/vm/address"
	"github.com/filecoin-project/go-filecoin/internal/pkg/wallet"
)

// StorageClientNodeConnector adapts the node to provide the correct interface to the storage client.
type StorageClientNodeConnector struct {
	connectorCommon

	clientAddr address.Address
	cborStore  cbor.IpldStore
}

var _ storagemarket.StorageClientNode = &StorageClientNodeConnector{}

// NewStorageClientNodeConnector creates a new connector
func NewStorageClientNodeConnector(
	cbor cbor.IpldStore,
	cs chainReader,
	w *msg.Waiter,
	wlt *wallet.Wallet,
	ob *message.Outbox,
	ca address.Address,
	wg WorkerGetter,
) *StorageClientNodeConnector {
	return &StorageClientNodeConnector{
		connectorCommon: connectorCommon{cs, w, wlt, ob, wg},
		cborStore:       cbor,
		clientAddr:      ca,
	}
}

// AddFunds sends a message to add collateral for the given address
func (s *StorageClientNodeConnector) AddFunds(ctx context.Context, addr address.Address, amount abi.TokenAmount) error {
	return s.addFunds(ctx, s.clientAddr, addr, amount)
}

// EnsureFunds checks the current balance for an address and adds funds if the balance is below the given amount
func (s *StorageClientNodeConnector) EnsureFunds(ctx context.Context, addr address.Address, amount abi.TokenAmount) error {
	balance, err := s.GetBalance(ctx, addr)
	if err != nil {
		return err
	}

	if !balance.Available.LessThan(amount) {
		return nil
	}

	return s.AddFunds(ctx, addr, big.Sub(amount, balance.Available))
}

// ListClientDeals returns all deals published on chain for the given account
func (s *StorageClientNodeConnector) ListClientDeals(ctx context.Context, addr address.Address) ([]storagemarket.StorageDeal, error) {
	return s.listDeals(ctx, addr)
}

// ListStorageProviders finds all miners that will provide storage
func (s *StorageClientNodeConnector) ListStorageProviders(ctx context.Context) ([]*storagemarket.StorageProviderInfo, error) {
	head := s.chainStore.Head()
	var spState spapow.State
	err := s.chainStore.GetActorStateAt(ctx, head, vmaddr.StoragePowerAddress, &spState)
	if err != nil {
		return nil, err
	}

	infos := []*storagemarket.StorageProviderInfo{}
	powerHamt, err := hamt.LoadNode(ctx, s.cborStore, spState.Claims)
	if err != nil {
		return nil, err
	}

	err = powerHamt.ForEach(ctx, func(minerAddrStr string, _ interface{}) error {
		minerAddr, err := address.NewFromString(minerAddrStr)
		if err != nil {
			return err
		}

		var mState spaminer.State
		err = s.chainStore.GetActorStateAt(ctx, head, minerAddr, &mState)
		if err != nil {
			return err
		}

		info := mState.Info
		infos = append(infos, &storagemarket.StorageProviderInfo{
			Address:    minerAddr,
			Owner:      info.Owner,
			Worker:     info.Worker,
			SectorSize: uint64(info.SectorSize),
			PeerID:     info.PeerId,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return infos, nil
}

// ValidatePublishedDeal validates a deal has been published correctly
// Adapted from https://github.com/filecoin-project/lotus/blob/3b34eba6124d16162b712e971f0db2ee108e0f67/markets/storageadapter/client.go#L156
func (s *StorageClientNodeConnector) ValidatePublishedDeal(ctx context.Context, deal storagemarket.ClientDeal) (uint64, error) {
	// Fetch receipt to return dealId
	chnMsg, found, err := s.waiter.Find(ctx, func(msg *types.SignedMessage, c cid.Cid) bool {
		return c.Equals(*deal.PublishMessage)
	})
	if err != nil {
		return 0, err
	}

	if !found {
		return 0, xerrors.Errorf("Could not find published deal message %s", deal.PublishMessage.String())
	}

	unsigned := chnMsg.Message.Message

	minerWorker, err := s.GetMinerWorker(ctx, deal.Proposal.Provider)
	if err != nil {
		return 0, err
	}

	if unsigned.From != minerWorker {
		return 0, xerrors.Errorf("deal wasn't published by storage provider: from=%s, provider=%s", unsigned.From, deal.Proposal.Provider)
	}

	if unsigned.To != vmaddr.StorageMarketAddress {
		return 0, xerrors.Errorf("deal publish message wasn't set to StorageMarket actor (to=%s)", unsigned.To)
	}

	if unsigned.Method != fcsm.PublishStorageDeals {
		return 0, xerrors.Errorf("deal publish message called incorrect method (method=%s)", unsigned.Method)
	}

	// TODO: Deserialize PublishStorageDealsParams instead of this
	values, err := fcabi.DecodeValues(unsigned.Params, []fcabi.Type{fcabi.StorageDealProposals})
	if err != nil {
		return 0, err
	}

	msgProposals := values[0].Val.([]market.ClientDealProposal)

	for idx, proposal := range msgProposals {
		if proposal.Proposal == deal.Proposal {
			sectorIDVal, err := fcabi.Deserialize(chnMsg.Receipt.Return[idx], fcabi.SectorID)
			if err != nil {
				return 0, err
			}

			sectorID, ok := sectorIDVal.Val.(uint64)
			if !ok {
				return 0, xerrors.New("publish deal return is not a sector ID")
			}
			return sectorID, nil
		}
	}

	return 0, xerrors.Errorf("published deal does not match ClientDeal")
}

// SignProposal uses the local wallet to sign the given proposal
func (s *StorageClientNodeConnector) SignProposal(ctx context.Context, signer address.Address, proposal market.DealProposal) (*market.ClientDealProposal, error) {
	buf := new(bytes.Buffer)
	if err := proposal.MarshalCBOR(buf); err != nil {
		return nil, err
	}

	signature, err := s.SignBytes(ctx, signer, buf.Bytes())
	if err != nil {
		return nil, err
	}

	return &market.ClientDealProposal{
		Proposal:        proposal,
		ClientSignature: *signature,
	}, nil
}

// GetDefaultWalletAddress returns the default account for this node
func (s *StorageClientNodeConnector) GetDefaultWalletAddress(ctx context.Context) (address.Address, error) {
	return s.clientAddr, nil
}

// ValidateAskSignature ensures the given ask has been signed correctly
func (s *StorageClientNodeConnector) ValidateAskSignature(signed *storagemarket.SignedStorageAsk) error {
	ask := signed.Ask

	buf := new(bytes.Buffer)
	if err := ask.MarshalCBOR(buf); err != nil {
		return err
	}

	if s.VerifySignature(*signed.Signature, ask.Miner, buf.Bytes()) {
		return nil
	}

	return xerrors.Errorf("invalid ask signature")
}
