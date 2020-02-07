package dispatch

import (
	"fmt"
	"github.com/filecoin-project/go-filecoin/internal/pkg/encoding"
	"github.com/filecoin-project/go-filecoin/internal/pkg/types"
	"github.com/ipfs/go-cid"
	"github.com/pkg/errors"
	"reflect"
)

// Actor is the interface all actors have to implement.
type Actor interface {
	// Exports has a list of method available on the actor.
	Exports() []interface{}
}

// Dispatch allows for dynamic method dispatching on an actor.
type Dispatch interface {
	// Dispatch will call the given method on the actor and pass the arguments.
	//
	// - The `ctx` argument will be coerced to the type the method expects in its first argument.
	// - If arg1 is `[]byte`, it will attempt to decode the value based on second argument in the target method.
	Dispatch(method types.MethodID, ctx interface{}, arg1 interface{}) (interface{}, error)
}

type actorDispatcher struct {
	code  cid.Cid
	actor Actor
}

type method interface {
	Call(in []reflect.Value) []reflect.Value
	Type() reflect.Type
}

var _ Dispatch = (*actorDispatcher)(nil)

// Dispatch implements `Dispatch`.
func (a actorDispatcher) Dispatch(methodID types.MethodID, ctx interface{}, arg1 interface{}) (interface{}, error) {
	exports := a.actor.Exports()

	// get method entry
	methodIdx := (uint64)(methodID)
	if len(exports) < (int)(methodIdx) {
		return nil, fmt.Errorf("Method undefined. method: %d, code: %s", methodID, a.code)
	}
	entry := exports[methodIdx]
	if entry == nil {
		return nil, fmt.Errorf("Method undefined. method: %d, code: %s", methodID, a.code)
	}

	// for the moment, we only support dispatching if the entry in the export table is a method pointer.
	m, ok := entry.(method)
	if !ok {
		// Note: the entry defined in the actor code is not a method pointer, check the `spec-actors` repo code.
		return nil, fmt.Errorf("Unsupported method definition. method: %d, code: %s", methodID, a.code)
	}

	// build args to pass to the method
	args := []reflect.Value{
		// the ctx will be automaticall coerced
		reflect.ValueOf(ctx),
	}

	if raw, ok := arg1.([]byte); ok {
		// decode arg1 before dispatching
		t := m.Type().In(1)
		v := reflect.New(t)
		if err := encoding.Decode(raw, v.Interface()); err != nil {
			return nil, errors.Wrap(err, "failed to decode arg1 during dispatch")
		}

		// push decoded arg to args list
		args = append(args, v)
	} else {
		// the argument was not in raw bytes, let it be coerced
		args = append(args, reflect.ValueOf(arg1))
	}

	// invoke the method
	out := m.Call(args)

	// Note: we only support single objects being returned
	if len(out) > 1 {
		return nil, fmt.Errorf("actor method returned more than one object. method: %d, code: %s", methodID, a.code)
	}

	// method returns unit
	if len(out) == 0 {
		return nil, nil
	}

	// forward return
	return out[1].Interface(), nil
}
