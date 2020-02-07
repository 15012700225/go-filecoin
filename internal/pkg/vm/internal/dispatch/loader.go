package dispatch

import (
	"fmt"

	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
	"github.com/ipfs/go-cid"
)

// codeVersion identifies an acor binary code by a cid and an epoch
type codeVersion struct {
	code  cid.Cid
	epoch types.BlockHeight
}

// CodeLoader allows yo to load an actor's code based on its id an epoch.
type CodeLoader struct {
	actors map[codeVersion]Actor
}

// GetActorImpl returns executable code for an actor by code cid at a specific protocol version
func (cl CodeLoader) GetActorImpl(code cid.Cid, epoch types.BlockHeight) (Dispatch, error) {
	// TODO: fix to allow lookup for the latest CID for the given epoch
	actor, ok := cl.actors[codeVersion{code: code, epoch: epoch}]
	if !ok {
		return nil, fmt.Errorf("Actor code not found. code: %s, epoch: %d", code, epoch)
	}
	return actorDispatcher{code: code, actor: actor}, nil
}

// CodeLoaderBuilder helps you build a CodeLoader.
type CodeLoaderBuilder struct {
	actors map[codeVersion]Actor
}

// NewBuilder creates a builder to generate a builtin.Actor data structure
func NewBuilder() *CodeLoaderBuilder {
	return &CodeLoaderBuilder{actors: map[codeVersion]Actor{}}
}

// Add lets you add an actor dispatch table for a given version.
func (b *CodeLoaderBuilder) Add(epoch types.BlockHeight, code cid.Cid, actor Actor) *CodeLoaderBuilder {
	b.actors[codeVersion{code: code, epoch: epoch}] = actor
	return b
}

// Build builds the code loader.
func (b *CodeLoaderBuilder) Build() CodeLoader {
	return CodeLoader{actors: b.actors}
}
