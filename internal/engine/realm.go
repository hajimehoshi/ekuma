// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Realm is a Realm Record.
type Realm struct {
	// context is shared by jobs bound to this realm.
	context ExecutionContext

	intrinsics [intrinsicCount]*Object

	exceptionReporter func(Value, ExceptionLocation)

	// asyncFromSyncProto is %AsyncFromSyncIteratorPrototype%, or nil until it is needed.
	asyncFromSyncProto *Object

	global *Object

	// globalThis is the global this value, which a host can set to a global proxy.
	globalThis *Object

	// globalProxyHandler is the private empty handler shared by realms that
	// can access each other's global objects.
	globalProxyHandler *Object

	// defaultGlobalProxyHandler is the realm's own access group, restored on detachment.
	defaultGlobalProxyHandler *Object

	// foreignGlobalView handles this realm's property access on foreign global proxies.
	foreignGlobalView *Object

	// globalLex holds the top-level let, const and class declarations.
	globalLex *Object

	shapes shapeTable

	arrayIter arrayIterCache

	regExpOriginals regExpOriginals

	// regExpBorrowers are the functions marked by [ExecutionContext.LendRegExpLiterals], and
	// regExpReceiverBorrowers those marked by [ExecutionContext.LendRegExpLiteralReceivers].
	regExpBorrowers, regExpReceiverBorrowers []*Object

	// evals caches the compiled code of eval.
	evals evalCache

	// referrer is the referrer of the code of the realm that has no ScriptOrModule.
	referrer *scriptOrModule

	// hostDefined is the [[HostDefined]] field of the realm.
	hostDefined any

	// extension is the state of the built-in extension in the realm.
	extension any

	// strictCallee is the callee property of unmapped arguments objects, or the zero value
	// until it is needed.
	strictCallee Value
}

func (ag *agent) newRealm() *Realm {
	r := &Realm{}
	r.context = ExecutionContext{
		agent: ag,
		realm: r,
	}
	r.referrer = &scriptOrModule{
		realm: r,
	}
	return r
}

// initRealmPrototypes creates the prototypes of the current realm that the strings, the keys
// and the other objects of the agent depend on.
func (cx *ExecutionContext) initRealmPrototypes() {
	r := cx.realm
	r.shapes.nullProto = cx.newShape(nil, r)
	r.shapes.proxy = cx.newShape(nil, r)
	objectProto := (&propertyObject{
		kind:  kindObject,
		shape: r.shapes.nullProto,
	}).init()
	r.intrinsics[IntrinsicObjectPrototype] = objectProto
	r.shapes.object = cx.shapeFor(objectProto)
	functionProto := (&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: r.shapes.object,
		fn: &function{
			native: func(cx *ExecutionContext, this Value, args []Value) Value {
				return Undefined
			},
			info: &nativeInfo{
				realm: r,
			},
		},
	}).init()
	r.intrinsics[IntrinsicFunctionPrototype] = functionProto
	r.shapes.function = cx.functionShapeFor(functionProto)
	arrayProto := (&propertyObject{
		kind:  kindArray,
		class: classArray,
		shape: cx.objectRootShape(objectProto, r, classArray),
	}).init()
	r.intrinsics[IntrinsicArrayPrototype] = arrayProto
	r.shapes.array = cx.objectRootShape(arrayProto, r, classArray)
	stringProto := cx.NewObject(objectProto)
	r.intrinsics[IntrinsicStringPrototype] = stringProto
	// A primitive string has its own length even when an ordinary object with the same
	// prototype inherits its length.
	r.shapes.string = cx.newShape(stringProto, r)
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
	stringProto.setExtState(cx.emptyString.o)
	cx.add(stringProto, cx.Key(KeyLength), Number(0), 0)
	r.shapes.iterResult = r.shapes.object.with(cx.Key(KeyValue), FlagsData).with(cx.Key(keyDone), FlagsData)
	r.shapes.arguments = r.shapes.object.with(cx.Key(KeyLength), FlagsHidden).with(cx.WellKnownSymbol(SymbolIterator), FlagsHidden)
	r.global = cx.NewObject(r.intrinsics[IntrinsicObjectPrototype])
	// Deleting a property can restore a shared shape and its cached global slot pointers.
	// Keep the slots in place until adding a property gives the global object a private shape.
	r.global.slots = make([]Value, 0, privateShapeThreshold)
	r.globalThis = r.global
	r.globalLex = cx.NewObject(nil)
}

// CreateRealm creates a realm whose built-in objects install creates while the realm is current.
func (ag *agent) CreateRealm(install func(*ExecutionContext)) *Realm {
	ag.trackJobRealms = true
	r := ag.newRealm()
	cx := r.Context()
	cx.Run(func() {
		cx.initRealmPrototypes()
		cx.initRealmGlobals()
		install(cx)
	})
	return r
}

// IsIntrinsicOfOtherRealm reports whether the function f is the intrinsic id of
// GetFunctionRealm(f), and that realm is not the current one.
func (cx *ExecutionContext) IsIntrinsicOfOtherRealm(f *Object, id Intrinsic) bool {
	r := cx.GetFunctionRealm(f)
	return r != cx.realm && r.intrinsics[id] == f
}

// CreationRealm returns the creation realm of the JavaScript object o, or nil for a detached global proxy.
func (o *Object) CreationRealm() *Realm {
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

// IsEnteredRealm reports whether r is the agent's entered realm.
func (ag *agent) IsEnteredRealm(r *Realm) bool {
	return r != nil && r == ag.enteredRealm
}

// SetRealmHostDefined sets the [[HostDefined]] field of the realm r to v.
func (ag *agent) SetRealmHostDefined(r *Realm, v any) {
	r.hostDefined = v
}

// RealmHostDefined returns the [[HostDefined]] field of the realm r.
func (ag *agent) RealmHostDefined(r *Realm) any {
	return r.hostDefined
}

// SetRealmExtension sets the state of the built-in extension of the current realm to v.
func (cx *ExecutionContext) SetRealmExtension(v any) {
	cx.realm.extension = v
}

// RealmExtension returns the state of the built-in extension of the realm r.
func (ag *agent) RealmExtension(r *Realm) any {
	return r.extension
}
