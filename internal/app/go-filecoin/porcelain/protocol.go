package porcelain

import (
	"context"
	"time"

	"github.com/pkg/errors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-filecoin/internal/pkg/block"
	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
)

// SectorInfo provides information about a sector construction
type SectorInfo struct {
	Size         *types.BytesAmount
	MaxPieceSize *types.BytesAmount
}

// ProtocolParams contains parameters that modify the filecoin nodes protocol
type ProtocolParams struct {
	Network          string
	AutoSealInterval uint
	BlockTime        time.Duration
	SupportedSectors []SectorInfo
}

type protocolParamsPlumbing interface {
	ConfigGet(string) (interface{}, error)
	ChainHeadKey() block.TipSetKey
	ProtocolStateView(baseKey block.TipSetKey) (ProtocolStateView, error)
	BlockTime() time.Duration
}

type ProtocolStateView interface {
	InitNetworkName(ctx context.Context) (string, error)
}

// ProtocolParameters returns protocol parameter information about the node
func ProtocolParameters(ctx context.Context, plumbing protocolParamsPlumbing) (*ProtocolParams, error) {
	autoSealIntervalInterface, err := plumbing.ConfigGet("mining.autoSealIntervalSeconds")
	if err != nil {
		return nil, err
	}

	autoSealInterval, ok := autoSealIntervalInterface.(uint)
	if !ok {
		return nil, errors.New("Failed to read autoSealInterval from config")
	}

	networkName, err := getNetworkName(ctx, plumbing)
	if err != nil {
		return nil, errors.Wrap(err, "could not retrieve network name")
	}

	sectorSizes := []*types.BytesAmount{types.OneKiBSectorSize, types.TwoHundredFiftySixMiBSectorSize}

	var supportedSectors []SectorInfo
	for _, sectorSize := range sectorSizes {
		maxUserBytes := types.NewBytesAmount(ffi.GetMaxUserBytesPerStagedSector(sectorSize.Uint64()))
		supportedSectors = append(supportedSectors, SectorInfo{sectorSize, maxUserBytes})
	}

	return &ProtocolParams{
		Network:          networkName,
		AutoSealInterval: autoSealInterval,
		BlockTime:        plumbing.BlockTime(),
		SupportedSectors: supportedSectors,
	}, nil
}

func getNetworkName(ctx context.Context, plumbing protocolParamsPlumbing) (string, error) {
	view, err := plumbing.ProtocolStateView(plumbing.ChainHeadKey())
	if err != nil {
		return "", errors.Wrap(err, "failed to query state")
	}
	return view.InitNetworkName(ctx)
}
