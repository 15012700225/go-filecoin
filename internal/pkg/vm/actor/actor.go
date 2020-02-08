// Package actor implements tooling to write and manipulate actors in go.
package actor

import (
	"fmt"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
)

// Actor is the central abstraction of entities in the system.
//
// Both individual accounts, as well as contracts (user & system level) are
// represented as actors. An actor has the following core functionality implemented on a system level:
// - track a Filecoin balance, using the `Balance` field
// - execute code stored in the `Code` field
// - read & write memory
// - replay protection, using the `Nonce` field
//
// Value sent to a non-existent address will be tracked as an empty actor that has a Balance but
// nil Code and Memory. You must nil check Code cids before comparing them.
//
// More specific capabilities for individual accounts or contract specific must be implemented
// inside the code.
//
// Not safe for concurrent access.
type Actor struct {
	// Code is a CID of the VM code for this actor's implementation (or a constant for actors implemented in Go code).
	// Code may be nil for an uninitialized actor (which exists because it has received a balance).
	Code cid.Cid `refmt:",omitempty"`
	// Head is the CID of the root of the actor's state tree.
	Head cid.Cid `refmt:",omitempty"`
	// CallSeqNum is the number expected on the next message from this actor.
	// Messages are processed in strict, contiguous order.
	CallSeqNum types.Uint64
	// Balance is the amount of FIL in the actor's account.
	Balance types.AttoFIL
}

// NewActor constructs a new actor.
func NewActor(code cid.Cid, balance types.AttoFIL) *Actor {
	return &Actor{
		Code:       code,
		Head:       cid.Undef,
		CallSeqNum: 0,
		Balance:    balance,
	}
}

// IsEmpty tests whether the actor's code is defined.
func (a *Actor) IsEmpty() bool {
	return !a.Code.Defined()
}

// IncrementSeqNum increments the seq number.
func (a *Actor) IncrementSeqNum() {
	a.CallSeqNum = a.CallSeqNum + 1
}

// Format implements fmt.Formatter.
func (a *Actor) Format(f fmt.State, c rune) {
	f.Write([]byte(fmt.Sprintf("<%s (%p); balance: %v; callSeqNum: %d>", types.ActorCodeTypeName(a.Code), a, a.Balance, a.CallSeqNum))) // nolint: errcheck
}
