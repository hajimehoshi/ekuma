// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// executionContextOf returns the execution context of the realm that embeds r.
func executionContextOf(r *realmPart) *ExecutionContext {
	// The two-value form compiles to no call, so that the interpreter loop makes none at a change of
	// realm.
	cx, _ := r.Owner.(*ExecutionContext)
	return cx
}

// contextOf returns the execution context of the realm that embeds r.
func (cx *ExecutionContext) contextOf(r *realmPart) *ExecutionContext {
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

	exceptionReporter func(Value, ExceptionLocation)

	// globalProxyHandler is the private empty handler shared by realms that
	// can access each other's global objects.
	globalProxyHandler *Object

	// defaultGlobalProxyHandler is the realm's own access group, restored on detachment.
	defaultGlobalProxyHandler *Object

	// foreignGlobalView handles this realm's property access on foreign global proxies.
	foreignGlobalView *Object

	// joins are the objects that Array.prototype.join is joining, and localeJoins those that
	// Array.prototype.toLocaleString is.
	joins, localeJoins joinGuard

	// regExpBorrowers are the functions marked by [ExecutionContext.LendRegExpLiterals], and
	// regExpReceiverBorrowers those marked by [ExecutionContext.LendRegExpLiteralReceivers].
	regExpBorrowers, regExpReceiverBorrowers []*Object

	// evals caches the compiled code of eval.
	evals evalCache

	// referrer is the referrer of the code of the realm that has no ScriptOrModule.
	referrer *scriptOrModule

	// hostDefined is the [[HostDefined]] field of the realm.
	hostDefined any

	// intlFallbackSymbol is the [[FallbackSymbol]] of %Intl%, or nil until it is needed.
	intlFallbackSymbol *Object
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
	r.Shapes.NullProto[ordinaryRoot] = cx.NewShape(nil, cx.realmPart, ordinaryRoot)
	r.Shapes.Proxy = cx.RootShape(nil, cx.realmPart, delegatedRoot)
	objectProto := storage.NewObject(r.Shapes.NullProto[ordinaryRoot])
	r.Intrinsics[IntrinsicObjectPrototype] = objectProto
	r.Shapes.Object = cx.ShapeFor(objectProto)
	functionProto := storage.NewFunctionObject(cx.FunctionShapeFor(objectProto), &function{
		Info: &nativeInfo{
			Native: nativeFunctionPrototype,
			Realm:  cx.realmPart,
		},
	})
	r.Intrinsics[IntrinsicFunctionPrototype] = functionProto
	r.Shapes.Function = cx.FunctionShapeFor(functionProto)
	r.Shapes.MaterializedFunction = cx.ShapeFor(functionProto)
	arrayProto := storage.NewArray(cx.RootShape(objectProto, cx.realmPart, arrayRoot), nil)
	r.Intrinsics[IntrinsicArrayPrototype] = arrayProto
	r.Shapes.Array = cx.RootShape(arrayProto, cx.realmPart, arrayRoot)
	stringProto := cx.NewObject(objectProto)
	r.Intrinsics[IntrinsicStringPrototype] = stringProto
	// A primitive string has its own length even when an ordinary object with the same
	// prototype inherits its length.
	r.Shapes.String = cx.RootShape(stringProto, cx.realmPart, primitiveStringRoot)
	r.Shapes.ShortStrings = new([shortstrings.MaxBytes + 1]Object)
	for i := range r.Shapes.ShortStrings {
		r.Shapes.ShortStrings[i] = Object{
			Kind:  kindString,
			Shape: r.Shapes.String,
			Text:  wtf8.String("\x00\x00\x00\x00\x00\x00\x00\x00")[:i],
		}
	}
}

// initRealmGlobals finishes the prototypes of the current realm and creates its global object.
func (cx *ExecutionContext) initRealmGlobals() {
	r := cx.realm
	stringProto := r.Intrinsics[IntrinsicStringPrototype]
	stringProto.Class = classString
	stringProto.SetExtState(cx.Strings.Empty.O)
	cx.Add(stringProto, cx.Key(KeyLength), storage.Number(0), 0)
	r.Shapes.IterResult = r.Shapes.Object.With(cx.Key(KeyValue), FlagsData).With(cx.Key(keyDone), FlagsData)
	r.Shapes.Arguments = r.Shapes.Object.With(cx.Key(KeyLength), FlagsHidden).With(cx.WellKnownSymbol(SymbolIterator), FlagsHidden)
	// Deleting a property can restore a shared shape and its cached global slot pointers.
	// Keep the slots in place until adding a property gives the global object a private shape.
	r.Global = storage.NewGlobalObject(cx.ShapeFor(r.Intrinsics[IntrinsicObjectPrototype]), privateShapeThreshold)
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
func (cx *ExecutionContext) IsIntrinsicOfOtherRealm(f *Object, id Intrinsic) bool {
	r := cx.FunctionContext(f).realm
	return r != cx.realm && r.Intrinsics[id] == f
}

// CreationContext returns the creation realm's context for the JavaScript object o,
// or nil for a detached global proxy.
func CreationContext(o *Object) *ExecutionContext {
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
