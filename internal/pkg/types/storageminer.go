package types

// SectorPreCommitInfo is a type which represents a storage miner's commitment
// to a particular chain when encoding a sector.
type SectorPreCommitInfo struct {
	SectorNumber Uint64
	CommR        []byte
	SealEpoch    Uint64
	DealIDs      []Uint64
}

// SectorProveCommitInfo is an on-chain type which represents a miner's proof
// of replication.
type SectorProveCommitInfo struct {
	Proof    []byte
	SectorID Uint64
	DealIDs  []Uint64
}
