package state

import (
	"context"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-hamt-ipld"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/pkg/errors"
)

// TreeLoader defines an interfaces for loading a state tree from an IpldStore.
type TreeLoader interface {
	// LoadStateTree loads the state tree referenced by the given cid.
	LoadStateTree(ctx context.Context, store cbor.IpldStore, c cid.Cid) (Tree, error)
}

type treeLoader struct{}

// NewTreeLoader creates a new `TreeLoader`
func NewTreeLoader() TreeLoader {
	return &treeLoader{}
}

var _ TreeLoader = &treeLoader{}

func (stl *treeLoader) LoadStateTree(ctx context.Context, store cbor.IpldStore, c cid.Cid) (Tree, error) {
	return loadStateTree(ctx, store, c)
}

func loadStateTree(ctx context.Context, store cbor.IpldStore, c cid.Cid) (Tree, error) {
	// TODO ideally this assertion can go away when #3078 lands in go-ipld-cbor
	root, err := hamt.LoadNode(ctx, store.(cbor.IpldStore), c, hamt.UseTreeBitWidth(TreeBitWidth))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load node for %s", c)
	}
	stateTree := newEmptyStateTree(store.(cbor.IpldStore))
	stateTree.root = root

	return stateTree, nil
}
