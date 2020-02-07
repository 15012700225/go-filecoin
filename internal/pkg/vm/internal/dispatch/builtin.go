package dispatch

import (
	"github.com/filecoin-project/specs-actors/actors/builtin/init"
	"github.com/filecoin-project/specs-actors/actors/builtin"
)

// DefaultActors is list of all actors that ship with Filecoin.
// They are indexed by their CID.
var DefaultActors = NewBuilder().
	// Add(0, codes.SystemActorCodeID, &NotInvocable{}).
	Add(0, builtin.InitActorCodeID, &init.InitActor{}).
	Build()
