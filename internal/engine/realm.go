// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// executionContextOf returns the execution context of the realm that embeds r.
func executionContextOf(r *storage.RealmPart) *ExecutionContext {
	// The two-value form compiles to no call, so that the interpreter loop makes none at a change of
	// realm.
	cx, _ := r.Owner.(*ExecutionContext)
	return cx
}

// contextOf returns the execution context of the realm that embeds r.
func (cx *ExecutionContext) contextOf(r *storage.RealmPart) *ExecutionContext {
	if r == cx.realmPart {
		return cx
	}
	return executionContextOf(r)
}

// realm is a Realm Record.
type realm struct {
	realmPart

	// context is shared by jobs bound to this realm.
	context ExecutionContext

	initBinaryBuiltins func(*ExecutionContext)

	exceptionReporter func(storage.Value, ExceptionLocation)

	// globalProxyHandler is the private empty handler shared by realms that
	// can access each other's global objects.
	globalProxyHandler *storage.Object

	// defaultGlobalProxyHandler is the realm's own access group, restored on detachment.
	defaultGlobalProxyHandler *storage.Object

	// foreignGlobalView handles this realm's property access on foreign global proxies.
	foreignGlobalView *storage.Object

	// joins are the objects that Array.prototype.join is joining, and localeJoins those that
	// Array.prototype.toLocaleString is.
	joins, localeJoins joinGuard

	// regExpBorrowers are the functions marked by [ExecutionContext.LendRegExpLiterals], and
	// regExpReceiverBorrowers those marked by [ExecutionContext.LendRegExpLiteralReceivers].
	regExpBorrowers, regExpReceiverBorrowers []*storage.Object

	// evals caches the compiled code of eval.
	evals evalCache

	// referrer is the referrer of the code of the realm that has no ScriptOrModule.
	referrer *scriptOrModule

	// hostDefined is the [[HostDefined]] field of the realm.
	hostDefined any

	// intlFallbackSymbol is the [[FallbackSymbol]] of %Intl%, or nil until it is needed.
	intlFallbackSymbol *storage.Object

	// concatMemo holds the last strings that concatenations of the realm made.
	concatMemo concatMemo
}

func (ag *agent) newRealm() *realm {
	r := &realm{}
	r.context = ExecutionContext{
		agent:     ag,
		realm:     r,
		realmPart: &r.realmPart,
		space:     &ag.space,
	}
	r.Owner = &r.context
	r.Space = &ag.space
	r.referrer = &scriptOrModule{
		realm: r,
	}
	return r
}

// initRealmPrototypes creates the prototypes of the current realm that the strings, the keys
// and the other objects of the agent depend on.
func (cx *ExecutionContext) initRealmPrototypes() {
	r := cx.realm
	r.Shapes.NullProto[storage.OrdinaryRoot] = cx.NewShape(nil, cx.realmPart, storage.OrdinaryRoot)
	r.Shapes.Proxy = cx.RootShape(nil, cx.realmPart, storage.DelegatedRoot)
	objectProto := storage.NewObject(r.Shapes.NullProto[storage.OrdinaryRoot])
	r.Intrinsics[storage.IntrinsicObjectPrototype] = objectProto
	r.Shapes.Object = cx.ShapeFor(objectProto)
	functionProto := storage.NewFunctionObject(cx.FunctionShapeFor(objectProto), &storage.Function{
		Info: &storage.NativeInfo{
			Native: nativeFunctionPrototype,
			Realm:  cx.realmPart,
		},
	})
	r.Intrinsics[storage.IntrinsicFunctionPrototype] = functionProto
	r.Shapes.Function = cx.FunctionShapeFor(functionProto)
	r.Shapes.MaterializedFunction = cx.ShapeFor(functionProto)
	arrayProto := storage.NewArray(cx.RootShape(objectProto, cx.realmPart, storage.ArrayRoot), nil)
	r.Intrinsics[storage.IntrinsicArrayPrototype] = arrayProto
	r.Shapes.Array = cx.RootShape(arrayProto, cx.realmPart, storage.ArrayRoot)
	stringProto := cx.NewObject(objectProto)
	r.Intrinsics[storage.IntrinsicStringPrototype] = stringProto
	// A primitive string has its own length even when an ordinary object with the same
	// prototype inherits its length.
	r.Shapes.String = cx.RootShape(stringProto, cx.realmPart, storage.PrimitiveStringRoot)
	r.InitShortStrings()
}

// initRealmGlobals finishes the prototypes of the current realm and creates its global object.
func (cx *ExecutionContext) initRealmGlobals() {
	r := cx.realm
	stringProto := r.Intrinsics[storage.IntrinsicStringPrototype]
	stringProto.Class = storage.ClassString
	stringProto.SetExtState(cx.Strings.Empty.O)
	cx.Add(stringProto, cx.Key(storage.KeyLength), storage.Number(0), 0)
	r.Shapes.IterResult = r.Shapes.Object.With(cx.Key(storage.KeyValue), storage.FlagsData).With(cx.Key(storage.KeyDone), storage.FlagsData)
	r.Shapes.Arguments = r.Shapes.Object.With(cx.Key(storage.KeyLength), storage.FlagsHidden).With(cx.WellKnownSymbol(storage.SymbolIterator), storage.FlagsHidden)
	// Deleting a property can restore a shared shape and its cached global slot pointers.
	// Keep the slots in place until adding a property gives the global object a private shape.
	r.Global = storage.NewGlobalObject(cx.ShapeFor(r.Intrinsics[storage.IntrinsicObjectPrototype]), storage.PrivateShapeThreshold)
	r.GlobalThis = r.Global
	r.GlobalLex = storage.NewGlobalObject(cx.ShapeFor(nil), 0)
}

// CreateRealm returns the context of a new realm after calling install with that context active.
func (ag *agent) CreateRealm(install func(*ExecutionContext)) *ExecutionContext {
	ag.trackJobRealms = true
	r := ag.newRealm()
	cx := &r.context
	cx.Run(func() {
		cx.initRealmPrototypes()
		cx.initRealmGlobals()
		install(cx)
	})
	return cx
}

// IsIntrinsicOfOtherRealm reports whether the function f is the intrinsic id of
// GetFunctionRealm(f), and that realm is not the current one.
func (cx *ExecutionContext) IsIntrinsicOfOtherRealm(f *storage.Object, id storage.Intrinsic) bool {
	r := cx.FunctionContext(f).realm
	return r != cx.realm && r.Intrinsics[id] == f
}

// CreationContext returns the creation realm's context for the JavaScript object o,
// or nil for a detached global proxy.
func CreationContext(o *storage.Object) *ExecutionContext {
	if r := o.CreationRealm(); r != nil {
		return executionContextOf(r)
	}
	return nil
}

// IsEnteredRealm reports whether the execution realm is the agent's entered realm.
func (cx *ExecutionContext) IsEnteredRealm() bool {
	return cx.realm == cx.enteredRealm
}

// SetRealmHostDefined sets the [[HostDefined]] field of the execution realm to v.
func (cx *ExecutionContext) SetRealmHostDefined(v any) {
	cx.realm.hostDefined = v
}

// RealmHostDefined returns the [[HostDefined]] field of the execution realm.
func (cx *ExecutionContext) RealmHostDefined() any {
	return cx.realm.hostDefined
}
