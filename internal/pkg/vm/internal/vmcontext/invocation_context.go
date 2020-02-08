package vmcontext

import (
	"context"
	"fmt"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/go-filecoin/internal/pkg/encoding"
	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/actor"
	vmaddr "github.com/filecoin-project/go-filecoin/internal/pkg/vm/address"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/internal/exitcode"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/internal/gas"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/internal/gascost"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/internal/message"
	"github.com/filecoin-project/go-filecoin/internal/pkg/vm/internal/runtime"
	notinit "github.com/filecoin-project/specs-actors/actors/builtin/init"
	"github.com/ipfs/go-cid"
)

type invocationContext struct {
	rt                *VM
	msg               internalMessage
	fromActor         *actor.Actor
	gasTank           *gas.Tracker
	isCallerValidated bool
	allowSideEffects  bool
	toActor           *actor.Actor
	stateHandle       internalActorStateHandle
}

type internalActorStateHandle interface {
	runtime.ActorStateHandle
	Validate()
}

func newInvocationContext(rt *VM, msg internalMessage, fromActor *actor.Actor, gasTank *gas.Tracker) invocationContext {
	// Note: the toActor and stateHandle are loaded during the `invoke()`
	return invocationContext{
		rt:                rt,
		msg:               msg,
		fromActor:         fromActor,
		gasTank:           gasTank,
		isCallerValidated: false,
		allowSideEffects:  true,
	}
}

type stateHandleContext invocationContext

func (ctx *stateHandleContext) AllowSideEffects(allow bool) {
	ctx.allowSideEffects = allow
}

func (ctx *stateHandleContext) Storage() runtime.Storage {
	return ctx.rt.Storage()
}

func (ctx *invocationContext) invoke() interface{} {
	// pre-dispatch
	// 1. charge gas for message invocation
	// 2. load target actor
	// 3. transfer optional funds
	// 4. short-circuit _Send_ method
	// 5. load target actor code
	// 6. create target state handle

	// assert from address is an ID address.
	runtime.Assert(ctx.msg.from.Protocol() == address.ID)

	// 1. charge gas for msg
	ctx.gasTank.Charge(gascost.OnMethodInvocation(&ctx.msg))

	// 2. load target actor
	// Note: we replace the "to" address with the normalized version
	ctx.toActor, ctx.msg.to = ctx.resolveTarget(ctx.msg.to)

	// 3. transfer funds carried by the msg
	ctx.rt.transfer(ctx.msg.from, ctx.msg.to, ctx.msg.value)

	// 4. if we are just sending funds, there is nothing else to do.
	if ctx.msg.method == types.SendMethodID {
		return message.Ok().WithGas(ctx.gasTank.GasConsumed())
	}

	// 5. load target actor code
	actorImpl := ctx.rt.getActorImpl(ctx.toActor.Code)

	// 6. create target state handle
	stateHandle := newActorStateHandle((*stateHandleContext)(ctx), ctx.toActor.Head)
	ctx.stateHandle = &stateHandle

	// dispatch
	ret, err := actorImpl.Dispatch(ctx.msg.method, ctx, ctx.msg.params)
	if err != nil {
		// Dragons: this is wrong, it could be a decoding error too
		runtime.Abort(exitcode.InvalidMethod)
	}

	// post-dispatch
	// 1. check caller was validated
	// 2. check state manipulation was valid
	// 3. success!

	// 1. check caller was validated
	if !ctx.isCallerValidated {
		runtime.Abortf(exitcode.MethodAbort, "Caller MUST be validated during method execution")
	}

	// 2. validate state access
	ctx.stateHandle.Validate()

	ctx.toActor.Head = stateHandle.head

	// 3. success!
	return ret
}

// resolveTarget loads and actor and returns its ActorID address.
//
// If the target actor does not exist, and the target address is a pub-key address,
// a new account actor will be created.
// Otherwise, this method will abort execution.
func (ctx *invocationContext) resolveTarget(target address.Address) (*actor.Actor, address.Address) {
	// resolve the target address via the InitActor, and attempt to load state.
	initActorEntry, err := ctx.rt.state.GetActor(context.Background(), vmaddr.InitAddress)
	if err != nil {
		panic(fmt.Errorf("init actor not found. %s", err))
	}

	// build state handle
	var stateHandle = NewReadonlyStateHandle(ctx.rt.Storage(), initActorEntry.Head)

	// get a view into the actor state
	var initState notinit.State
	stateHandle.Readonly(&initState)

	// lookup the ActorID based on the address
	targetIDAddr, err := initState.ResolveAddress(ctx.rt.Storage(), target)
	if err != nil {
		targetActor, err := ctx.rt.state.GetActor(context.Background(), targetIDAddr)
		if err == nil {
			// actor found, return it and its IDAddress
			return targetActor, targetIDAddr
		}
	}

	// actor does not exist, create an account actor
	// - precond: address must be a pub-key
	// - sent init actor a msg to create the new account

	if target.Protocol() != address.SECP256K1 && target.Protocol() != address.BLS {
		// Don't implicitly create an account actor for an address without an associated key.
		runtime.Abort(exitcode.ActorNotFound)
	}

	// send init actor msg to create the account actor
	constructorParams, err := encoding.Encode(target)
	if err != nil {
		runtime.Abortf(exitcode.EncodingError, "failed to encode params. %s", err)
	}

	params := notinit.ExecParams{CodeCID: types.AccountActorCodeCid, ConstructorParams: constructorParams}
	encodedParams, err := encoding.Encode(params)
	if err != nil {
		runtime.Abortf(exitcode.EncodingError, "failed to encode params. %sß", err)
	}
	newMsg := internalMessage{
		from:   ctx.msg.from,
		to:     vmaddr.InitAddress,
		value:  types.ZeroAttoFIL,
		method: initactor.ExecMethodID,
		params: encodedParams,
	}

	newCtx := newInvocationContext(ctx.rt, newMsg, ctx.fromActor, ctx.gasTank)

	targetIDAddrOpaque := newCtx.invoke()
	// cast response, interface{} -> address.Address
	targetIDAddr = targetIDAddrOpaque.(address.Address)

	// load actor
	targetActor, err := ctx.rt.state.GetActor(context.Background(), targetIDAddr)
	if err != nil {
		panic(fmt.Errorf("unreachable: exec failed to create the actor but returned successfully. %s", err))
	}

	return targetActor, targetIDAddr
}

//
// implement runtime.InvocationContext for invocationContext
//

var _ runtime.InvocationContext = (*invocationContext)(nil)

// Runtime implements runtime.InvocationContext.
func (ctx *invocationContext) Runtime() runtime.Runtime {
	return ctx.rt
}

// Message implements runtime.InvocationContext.
func (ctx *invocationContext) Message() runtime.MessageInfo {
	return ctx.msg
}

// ValidateCaller implements runtime.InvocationContext.
func (ctx *invocationContext) ValidateCaller(pattern runtime.CallerPattern) {
	if ctx.isCallerValidated {
		runtime.Abortf(exitcode.MethodAbort, "Method must validate caller identity exactly once")
	}
	if !pattern.IsMatch((*patternContext2)(ctx)) {
		runtime.Abortf(exitcode.MethodAbort, "Method invoked by incorrect caller")
	}
	ctx.isCallerValidated = true
}

// StateHandle implements runtime.InvocationContext.
func (ctx *invocationContext) StateHandle() runtime.ActorStateHandle {
	return ctx.stateHandle
}

// Send implements runtime.InvocationContext.
func (ctx *invocationContext) Send(to address.Address, method types.MethodID, value types.AttoFIL, params interface{}) interface{} {
	// check if side-effects are allowed
	if !ctx.allowSideEffects {
		runtime.Abortf(exitcode.MethodAbort, "Calling Send() is not allowed during side-effet lock")
	}

	// prepare
	// 1. alias fromActor
	// 2. build internal message

	// 1. fromActor = executing toActor
	from := ctx.msg.to
	fromActor := ctx.toActor

	// 2. build internal message
	encodedParams, err := encoding.Encode(params)
	if err != nil {
		runtime.Abortf(exitcode.EncodingError, "failed to encode params. %s", err)
	}
	newMsg := internalMessage{
		from:   from,
		to:     to,
		value:  value,
		method: method,
		params: encodedParams,
	}

	// invoke
	// 1. build new context
	// 2. invoke message
	// 3. success!

	// 1. build new context
	newCtx := newInvocationContext(ctx.rt, newMsg, fromActor, ctx.gasTank)

	// 2. invoke
	ret := newCtx.invoke()

	// 3. success!
	return ret
}

/// Balance implements runtime.InvocationContext.
func (ctx *invocationContext) Balance() types.AttoFIL {
	return ctx.toActor.Balance
}

// Charge implements runtime.InvocationContext.
func (ctx *invocationContext) Charge(cost types.GasUnits) error {
	ctx.gasTank.Charge(cost)
	return nil
}

//
// implement runtime.InvocationContext for invocationContext
//

var _ runtime.ExtendedInvocationContext = (*invocationContext)(nil)

/// CreateActor implements runtime.ExtendedInvocationContext.
func (ctx *invocationContext) CreateActor(code cid.Cid, idAddr address.Address) {
	if !isBuiltinActor(code) {
		runtime.Abortf(exitcode.MethodAbort, "Can only create built-in actors.")
	}

	if isSingletonActor(code) {
		runtime.Abortf(exitcode.MethodAbort, "Can only have one instance of singleton actors.")
	}

	// Check existing address. If nothing there, create empty actor.
	//
	// Note: we are storing the actors by ActorID *address*
	newActor, _, err := ctx.rt.state.GetOrCreateActor(context.TODO(), idAddr, func() (*actor.Actor, address.Address, error) {
		return &actor.Actor{}, idAddr, nil
	})

	if err != nil {
		runtime.Abortf(exitcode.MethodAbort, "Could not get or create actor")
	}

	if !newActor.Empty() {
		runtime.Abortf(exitcode.MethodAbort, "Actor address already exists")
	}

	// make this the right 'type' of actor
	newActor.Code = code
}

/// VerifySignature implements runtime.ExtendedInvocationContext.
func (ctx *invocationContext) VerifySignature(signer address.Address, signature types.Signature, msg []byte) bool {
	return types.IsValidSignature(msg, signer, signature)
}

// patternContext implements the PatternContext
type patternContext2 invocationContext

var _ runtime.PatternContext = (*patternContext2)(nil)

func (ctx *patternContext2) Code() cid.Cid {
	return ctx.fromActor.Code
}

func isBuiltinActor(code cid.Cid) bool {
	return code.Equals(types.AccountActorCodeCid) ||
		code.Equals(types.StorageMarketActorCodeCid) ||
		code.Equals(types.InitActorCodeCid) ||
		code.Equals(types.MinerActorCodeCid) ||
		code.Equals(types.BootstrapMinerActorCodeCid)
}

func isSingletonActor(code cid.Cid) bool {
	return code.Equals(types.StorageMarketActorCodeCid) ||
		code.Equals(types.InitActorCodeCid)
}
