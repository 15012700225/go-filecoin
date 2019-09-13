package testhelpers

import (
	"context"
	"testing"

	"github.com/filecoin-project/go-filecoin/actor"
	"github.com/filecoin-project/go-filecoin/address"
	"github.com/filecoin-project/go-filecoin/processor"
	"github.com/filecoin-project/go-filecoin/state"
	"github.com/filecoin-project/go-filecoin/types"
	"github.com/filecoin-project/go-filecoin/vm"
	"github.com/ipfs/go-cid"
)

// MakeRandomPoStProofForTest creates a random proof.
func MakeRandomPoStProofForTest() types.PoStProof {
	proofSize := types.OnePoStProofPartition.ProofLen()
	p := MakeRandomBytes(proofSize)
	p[0] = 42
	poStProof := make([]byte, proofSize)
	for idx, elem := range p {
		poStProof[idx] = elem
	}
	return poStProof
}

// TestSignedMessageValidator is a validator that doesn't validate to simplify message creation in tests.
type TestSignedMessageValidator struct{}

var _ processor.SignedMessageValidator = (*TestSignedMessageValidator)(nil)

// Validate always returns nil
func (tsmv *TestSignedMessageValidator) Validate(ctx context.Context, msg *types.SignedMessage, fromActor *actor.Actor) error {
	return nil
}

// TestBlockRewarder is a rewarder that doesn't actually add any rewards to simplify state tracking in tests
type TestBlockRewarder struct{}

var _ processor.BlockRewarder = (*TestBlockRewarder)(nil)

// BlockReward is a noop
func (tbr *TestBlockRewarder) BlockReward(ctx context.Context, st state.Tree, minerAddr address.Address) error {
	// do nothing to keep state root the same
	return nil
}

// GasReward does nothing
func (tbr *TestBlockRewarder) GasReward(ctx context.Context, st state.Tree, minerAddr address.Address, msg *types.SignedMessage, cost types.AttoFIL) error {
	// do nothing to keep state root the same
	return nil
}

// FakeBlockValidator passes everything as valid
type FakeBlockValidator struct{}

// NewFakeBlockValidator createas a FakeBlockValidator that passes everything as valid.
func NewFakeBlockValidator() *FakeBlockValidator {
	return &FakeBlockValidator{}
}

// ValidateSemantic does nothing.
func (fbv *FakeBlockValidator) ValidateSemantic(ctx context.Context, child *types.Block, parents *types.TipSet) error {
	return nil
}

// ValidateSyntax does nothing.
func (fbv *FakeBlockValidator) ValidateSyntax(ctx context.Context, blk *types.Block) error {
	return nil
}

// ValidateMessagesSyntax does nothing
func (fbv *FakeBlockValidator) ValidateMessagesSyntax(ctx context.Context, messages []*types.SignedMessage) error {
	return nil
}

// ValidateReceiptsSyntax does nothing
func (fbv *FakeBlockValidator) ValidateReceiptsSyntax(ctx context.Context, receipts []*types.MessageReceipt) error {
	return nil
}

// StubBlockValidator is a mockable block validator.
type StubBlockValidator struct {
	syntaxStubs   map[cid.Cid]error
	semanticStubs map[cid.Cid]error
}

// NewStubBlockValidator creates a StubBlockValidator that allows errors to configured
// for blocks passed to the Validate* methods.
func NewStubBlockValidator() *StubBlockValidator {
	return &StubBlockValidator{
		syntaxStubs:   make(map[cid.Cid]error),
		semanticStubs: make(map[cid.Cid]error),
	}
}

// ValidateSemantic returns nil or error for stubbed block `child`.
func (mbv *StubBlockValidator) ValidateSemantic(ctx context.Context, child *types.Block, parents *types.TipSet) error {
	return mbv.semanticStubs[child.Cid()]
}

// ValidateSyntax return nil or error for stubbed block `blk`.
func (mbv *StubBlockValidator) ValidateSyntax(ctx context.Context, blk *types.Block) error {
	return mbv.syntaxStubs[blk.Cid()]
}

// StubSyntaxValidationForBlock stubs an error when the ValidateSyntax is called
// on the with the given block.
func (mbv *StubBlockValidator) StubSyntaxValidationForBlock(blk *types.Block, err error) {
	mbv.syntaxStubs[blk.Cid()] = err
}

// StubSemanticValidationForBlock stubs an error when the ValidateSemantic is called
// on the with the given child block.
func (mbv *StubBlockValidator) StubSemanticValidationForBlock(child *types.Block, err error) {
	mbv.semanticStubs[child.Cid()] = err
}

// NewTestProcessor creates a processor with a test validator and test rewarder
func NewTestProcessor() *processor.DefaultProcessor {
	return processor.NewConfiguredProcessor(&TestSignedMessageValidator{}, &TestBlockRewarder{})
}

type testSigner struct{}

func (ms testSigner) SignBytes(data []byte, addr address.Address) (types.Signature, error) {
	return types.Signature{}, nil
}

// ApplyTestMessage sends a message directly to the vm, bypassing message
// validation
func ApplyTestMessage(st state.Tree, store vm.StorageMap, msg *types.Message, bh *types.BlockHeight) (*processor.ApplicationResult, error) {
	return applyTestMessageWithAncestors(st, store, msg, bh, nil)
}

// ApplyTestMessageWithGas uses the TestBlockRewarder but the default SignedMessageValidator
func ApplyTestMessageWithGas(st state.Tree, store vm.StorageMap, msg *types.Message, bh *types.BlockHeight, signer *types.MockSigner,
	gasPrice types.AttoFIL, gasLimit types.GasUnits, minerOwner address.Address) (*processor.ApplicationResult, error) {

	smsg, err := types.NewSignedMessage(*msg, signer, gasPrice, gasLimit)
	if err != nil {
		panic(err)
	}
	applier := processor.NewConfiguredProcessor(processor.NewDefaultMessageValidator(), processor.NewDefaultBlockRewarder())
	return newMessageApplier(smsg, applier, st, store, bh, minerOwner, nil)
}

func newMessageApplier(smsg *types.SignedMessage, processor *processor.DefaultProcessor, st state.Tree, storageMap vm.StorageMap,
	bh *types.BlockHeight, minerOwner address.Address, ancestors []types.TipSet) (*processor.ApplicationResult, error) {
	amr, err := processor.ApplyMessagesAndPayRewards(context.Background(), st, storageMap, []*types.SignedMessage{smsg}, minerOwner, bh, ancestors)

	if len(amr.Results) > 0 {
		return amr.Results[0], err
	}

	return nil, err
}

// CreateAndApplyTestMessageFrom wraps the given parameters in a message and calls ApplyTestMessage.
func CreateAndApplyTestMessageFrom(t *testing.T, st state.Tree, vms vm.StorageMap, from address.Address, to address.Address, val, bh uint64, method string, ancestors []types.TipSet, params ...interface{}) (*processor.ApplicationResult, error) {
	t.Helper()

	pdata := actor.MustConvertParams(params...)
	msg := types.NewMessage(from, to, 0, types.NewAttoFILFromFIL(val), method, pdata)
	return applyTestMessageWithAncestors(st, vms, msg, types.NewBlockHeight(bh), ancestors)
}

// CreateAndApplyTestMessage wraps the given parameters in a message and calls
// CreateAndApplyTestMessageFrom sending the message from address.TestAddress
func CreateAndApplyTestMessage(t *testing.T, st state.Tree, vms vm.StorageMap, to address.Address, val, bh uint64, method string, ancestors []types.TipSet, params ...interface{}) (*processor.ApplicationResult, error) {
	return CreateAndApplyTestMessageFrom(t, st, vms, address.TestAddress, to, val, bh, method, ancestors, params...)
}

func applyTestMessageWithAncestors(st state.Tree, store vm.StorageMap, msg *types.Message, bh *types.BlockHeight, ancestors []types.TipSet) (*processor.ApplicationResult, error) {
	smsg, err := types.NewSignedMessage(*msg, testSigner{}, types.NewGasPrice(1), types.NewGasUnits(300))
	if err != nil {
		panic(err)
	}

	ta := newTestApplier()
	return newMessageApplier(smsg, ta, st, store, bh, address.Undef, ancestors)
}

func newTestApplier() *processor.DefaultProcessor {
	return processor.NewConfiguredProcessor(&TestSignedMessageValidator{}, &TestBlockRewarder{})
}