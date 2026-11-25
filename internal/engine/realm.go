// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// realmPart is the storage state of a realm, which the realm embeds.
type realmPart struct {
	intrinsics [intrinsicCount]*Object

	shapes shapeTable

	arrayIter arrayIterCache

	regExpOriginals regExpOriginals

	global *Object

	// globalThis is the global this value, which a host can set to a global proxy.
	globalThis *Object

	// globalLex holds the top-level let, const and class declarations.
	globalLex *Object

	// objectTagStrings are results of Object.prototype.toString, by tag.
	objectTagStrings map[wtf8.String]Value

	// strictCallee is the callee property of unmapped arguments objects, or the zero value
	// until it is needed.
	strictCallee Value

	// regExpFlagStrings holds the value of the flags getter for each set of the 8 flags, once
	// computed. It is nil until it is needed.
	regExpFlagStrings *[1 << 8]Value

	// asyncFromSyncProto is %AsyncFromSyncIteratorPrototype%, or nil until it is needed.
	asyncFromSyncProto *Object

	// owner is the execution context of the realm. Only executionContext reads it.
	owner any
}

// executionContext returns the execution context of the realm that embeds r.
func (r *realmPart) executionContext() *ExecutionContext {
	// The two-value form compiles to no call, which the frame changes of the interpreter loop rely on.
	cx, _ := r.owner.(*ExecutionContext)
	return cx
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

	legacyRegExp legacyRegExpStatics
}

func (ag *agent) newRealm() *realm {
	r := &realm{}
	r.context = ExecutionContext{
		agent:     ag,
		realm:     r,
		realmPart: &r.realmPart,
		space:     &ag.space,
	}
	r.owner = &r.context
	r.referrer = &scriptOrModule{
		realm: r,
	}
	return r
}

// initRealmPrototypes creates the prototypes of the current realm that the strings, the keys
// and the other objects of the agent depend on.
func (cx *ExecutionContext) initRealmPrototypes() {
	r := cx.realm
	r.shapes.nullProto[ordinaryRoot] = cx.newShape(nil, cx.realmPart, ordinaryRoot)
	r.shapes.proxy = cx.rootShape(nil, cx.realmPart, delegatedRoot)
	objectProto := (&propertyObject{
		kind:  kindObject,
		shape: r.shapes.nullProto[ordinaryRoot],
	}).init()
	r.intrinsics[IntrinsicObjectPrototype] = objectProto
	r.shapes.object = cx.shapeFor(objectProto)
	functionProto := (&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: cx.functionShapeFor(objectProto),
		fn: &function{
			info: &nativeInfo{
				native: nativeFunctionPrototype,
				realm:  r,
			},
		},
	}).init()
	r.intrinsics[IntrinsicFunctionPrototype] = functionProto
	r.shapes.function = cx.functionShapeFor(functionProto)
	r.shapes.materializedFunction = cx.shapeFor(functionProto)
	arrayProto := (&propertyObject{
		kind:  kindArray,
		class: classArray,
		shape: cx.rootShape(objectProto, cx.realmPart, arrayRoot),
	}).init()
	r.intrinsics[IntrinsicArrayPrototype] = arrayProto
	r.shapes.array = cx.rootShape(arrayProto, cx.realmPart, arrayRoot)
	stringProto := cx.NewObject(objectProto)
	r.intrinsics[IntrinsicStringPrototype] = stringProto
	// A primitive string has its own length even when an ordinary object with the same
	// prototype inherits its length.
	r.shapes.string = cx.rootShape(stringProto, cx.realmPart, primitiveStringRoot)
	r.shapes.shortStrings = new([shortstrings.MaxBytes + 1]Object)
	for i := range r.shapes.shortStrings {
		r.shapes.shortStrings[i] = Object{
			kind:  kindString,
			shape: r.shapes.string,
			str:   wtf8.String("\x00\x00\x00\x00\x00\x00\x00\x00")[:i],
		}
	}
}

// initRealmGlobals finishes the prototypes of the current realm and creates its global object.
func (cx *ExecutionContext) initRealmGlobals() {
	r := cx.realm
	stringProto := r.intrinsics[IntrinsicStringPrototype]
	stringProto.class = classString
	stringProto.setExtState(cx.strings.empty.o)
	cx.add(stringProto, cx.Key(KeyLength), Number(0), 0)
	r.shapes.iterResult = r.shapes.object.with(cx.Key(KeyValue), FlagsData).with(cx.Key(keyDone), FlagsData)
	r.shapes.arguments = r.shapes.object.with(cx.Key(KeyLength), FlagsHidden).with(cx.WellKnownSymbol(SymbolIterator), FlagsHidden)
	r.global = cx.NewObject(r.intrinsics[IntrinsicObjectPrototype])
	// Deleting a property can restore a shared shape and its cached global slot pointers.
	// Keep the slots in place until adding a property gives the global object a private shape.
	r.global.initSlots(make([]Value, 0, privateShapeThreshold))
	r.global.pinned = pinnedGlobalSlots
	r.globalThis = r.global
	r.globalLex = cx.NewObject(nil)
	r.globalLex.pinned = pinnedGlobalSlots
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
	return r != cx.realm && r.intrinsics[id] == f
}

// CreationContext returns the creation realm's context for the JavaScript object o,
// or nil for a detached global proxy.
func (o *Object) CreationContext() *ExecutionContext {
	if r := o.creationRealm(); r != nil {
		return r.executionContext()
	}
	return nil
}

func (o *Object) creationRealm() *realmPart {
	if o.class == classProxy {
		st := o.extState().(*proxyState)
		if st.isGlobal() {
			if st.target == nil {
				return nil
			}
			o = st.target
		}
	}
	return o.shape.realm
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
