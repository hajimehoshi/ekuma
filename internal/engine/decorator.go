// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// This file implements the decorators proposal (https://github.com/tc39/proposal-decorators):
// auto-accessors, and the evaluation of classes that have decorators. It also implements the
// decorator metadata proposal (https://github.com/tc39/proposal-decorator-metadata).

// elemInitializer is the kind of a [storage.ClassElement] that is an initializer that a decorator of a
// method added. It runs on every instance before the fields are defined.
const elemInitializer = 16

// decoratorDef is a DecoratorDefinition Record.
type decoratorDef struct {
	decorator storage.Value
	receiver  storage.Value
}

// elementDef is a ClassElementDefinition Record, or a static block.
type elementDef struct {
	// kind is a bytecode Elem kind without [bytecode.ElemStatic].
	kind   int
	static bool
	key    storage.Value

	// value is the function of a method, a getter or a setter, the initializer of a field or
	// an auto-accessor, or nil, or the function of a static block.
	value *storage.Object

	// getter, setter and storage are the getter, the setter and the backing private name of an auto-accessor.
	getter, setter, storage *storage.Object

	decorators []decoratorDef
	dec        storage.FieldDecoration
}

// classDecoration is the state of the evaluation of a class that has decorators.
type classDecoration struct {
	name storage.Value

	// pending are the decorators of the element that is evaluated next, in source order.
	pending []decoratorDef

	classDecorators []decoratorDef
	elements        []*elementDef

	// metadata is the object that the context objects of the decorators share.
	metadata *storage.Object

	staticMethodInitializers []*storage.Object
	classInitializers        []*storage.Object
}

// decorationState is the Record that tells whether the call of a decorator has finished.
type decorationState struct {
	finished bool
}

// newClassDecoration returns a new state for the evaluation of the class with the name name.
func (ag *agent) newClassDecoration(name storage.Value) storage.Value {
	return storage.Value{O: storage.NewObjectWithState(nil, storage.ClassObject, &classDecoration{
		name: name,
	})}
}

// autoAccessorOp performs the instruction i, an [bytecode.OpAutoAccessor], on the registers regs.
func (cx *ExecutionContext) autoAccessorOp(regs []storage.Value, i *bytecode.Instr) {
	privateName, getter, setter := cx.makeAutoAccessor(regs[i.B])
	regs[i.A] = storage.Value{O: privateName}
	regs[i.A+1] = storage.Value{O: getter}
	regs[i.A+2] = storage.Value{O: setter}
}

// decorateOp performs the instruction i, one of [bytecode.OpDecorateBegin] through
// [bytecode.OpDecorateFinish], on the registers regs; x is the [bytecode.OpExtra] after an
// [bytecode.OpDecorateElement]. It returns the value for the register a of a step that has one.
// The registers may move when it returns.
func (cx *ExecutionContext) decorateOp(regs []storage.Value, i, x *bytecode.Instr) (storage.Value, bool) {
	switch i.Op {
	case bytecode.OpDecorateBegin:
		return cx.newClassDecoration(regs[i.B]), true
	case bytecode.OpDecorateApply:
		return cx.applyClassDecorators(regs[i.B].O.ExtState().(*classDecoration), regs[i.C].O), true
	}
	st := regs[i.A].O.ExtState().(*classDecoration)
	switch i.Op {
	case bytecode.OpDecorateAdd:
		st.pending = append(st.pending, decoratorDef{
			decorator: regs[i.B],
			receiver:  regs[i.C],
		})
	case bytecode.OpDecorateClass:
		st.classDecorators = st.pending
		st.pending = nil
	case bytecode.OpDecorateElement:
		var k, v storage.Value
		if i.B >= 0 {
			k = regs[i.B]
		}
		if i.C >= 0 {
			v = regs[i.C]
		}
		cx.addDecoratedElement(st, int(x.A), k, v.O)
	case bytecode.OpDecorateFinish:
		cx.finishClass(st, regs[i.B])
	}
	return storage.Undefined, false
}

// addDecoratedElement adds the class element of the Elem kind elem with the key k and the value
// or initializer v, which may be nil, to the state st, with the decorators added so far.
func (cx *ExecutionContext) addDecoratedElement(st *classDecoration, elem int, k storage.Value, v *storage.Object) {
	e := &elementDef{
		kind:       elem &^ bytecode.ElemStatic,
		static:     elem&bytecode.ElemStatic != 0,
		key:        k,
		value:      v,
		decorators: st.pending,
	}
	st.pending = nil
	if e.kind == bytecode.ElemAccessor {
		e.storage, e.getter, e.setter = cx.makeAutoAccessor(k)
	}
	st.elements = append(st.elements, e)
}

// makeAutoAccessor returns the backing private name, the getter and the setter of an
// auto-accessor with the key k, which is a property key or a private name.
func (cx *ExecutionContext) makeAutoAccessor(k storage.Value) (backing, getter, setter *storage.Object) {
	name := cx.funcNameOfKey(k)
	if len("get ")+len(name) > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	storageName := name
	if k.O.Class == storage.ClassPrivateName {
		storageName = k.O.Text
	}
	backing = storage.NewPrivateName(storageName + " accessor storage")
	getter = cx.NewNativeWithState("get "+name, 0, nativeAutoAccessorGetter, backing)
	setter = cx.NewNativeWithState("set "+name, 1, nativeAutoAccessorSetter, backing)
	return backing, getter, setter
}

// autoAccessorGetter is the code of the getters of auto-accessors. Their state is the backing
// private name, an *Object.
func autoAccessorGetter(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.PrivateGet(this, cx.ActiveNativeState().(*storage.Object))
}

// autoAccessorSetter is the code of the setters of auto-accessors. Their state is the backing
// private name, an *Object.
func autoAccessorSetter(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cx.PrivateSet(this, cx.ActiveNativeState().(*storage.Object), argAt(args, 0))
	return storage.Undefined
}

// PrivateGet implements PrivateGet.
func (cx *ExecutionContext) PrivateGet(o storage.Value, name *storage.Object) storage.Value {
	v, getter := cx.getPrivate(o, name)
	if getter != nil {
		return cx.Call(getter, o)
	}
	return v
}

// PrivateSet implements PrivateSet.
func (cx *ExecutionContext) PrivateSet(o storage.Value, name *storage.Object, v storage.Value) {
	if setter := cx.setPrivate(o, name, v); setter != nil {
		cx.Call(setter, o, v)
	}
}

// applyClassDecorators performs the steps of ClassDefinitionEvaluation from the application of
// the decorators of the elements to the application of the decorators of the class F, and
// returns the class that the decorators of the class returned.
func (cx *ExecutionContext) applyClassDecorators(st *classDecoration, f *storage.Object) storage.Value {
	proto := f.Fn.Home

	// Nothing has had access to F yet, so its [[Prototype]] is still constructorParent. Without
	// a ClassHeritage it is %Function.prototype%, whose non-configurable, non-writable
	// @@metadata is null, as the spec gives for that case.
	parent := cx.Get(f.Shape.Proto, storage.NamedKey(cx.WellKnownSymbol(storage.SymbolMetadata)))
	var metadataProto *storage.Object
	if parent.IsObject() {
		metadataProto = parent.O
	}
	st.metadata = cx.NewObject(metadataProto)
	var instanceMethodInitializers []*storage.Object

	home := func(e *elementDef) *storage.Object {
		if e.static {
			return f
		}
		return proto
	}
	isMethod := func(e *elementDef) bool {
		return e.kind != bytecode.ElemField && e.kind != bytecode.ElemStaticBlock
	}
	for _, static := range []bool{true, false} {
		for _, e := range st.elements {
			if e.static != static || !isMethod(e) {
				continue
			}
			extra := &instanceMethodInitializers
			switch {
			case e.kind == bytecode.ElemAccessor:
				extra = &e.dec.Extra
			case static:
				extra = &st.staticMethodInitializers
			}
			cx.applyDecoratorsToElementDefinition(e, extra, st.metadata)
			cx.defineMethodProperty(home(e), e)
		}
	}
	for _, static := range []bool{true, false} {
		for _, e := range st.elements {
			if e.static == static && e.kind == bytecode.ElemField {
				cx.applyDecoratorsToElementDefinition(e, &e.dec.Extra, st.metadata)
			}
		}
	}

	// The instance elements run in the order of InitializeInstanceElements: the private
	// methods, the initializers of the methods and then the fields.
	var fields []storage.ClassElement
	for _, init := range instanceMethodInitializers {
		fields = append(fields, storage.ClassElement{
			Kind: elemInitializer,
			Val:  init,
		})
	}
	for _, e := range st.elements {
		if !e.static {
			fields = e.appendElements(fields)
		}
	}
	if fields != nil {
		f.OwnFunction().Fields = &fields
	}

	// InitializePrivateMethods(F, staticElements).
	for _, e := range st.elements {
		if !e.static {
			continue
		}
		for _, c := range e.appendElements(nil) {
			if c.Kind != bytecode.ElemField {
				cx.defineClassElement(storage.Value{O: f}, &c)
			}
		}
	}

	newF := cx.applyDecoratorsToClassDefinition(st, storage.Value{O: f})
	cx.DefinePropertyOrThrow(newF.O, storage.NamedKey(cx.WellKnownSymbol(storage.SymbolMetadata)), storage.DataDesc(storage.Value{O: st.metadata}, storage.FlagsHidden))
	return newF
}

// appendElements appends the elements that the element e adds to an object to elems: the
// private methods and accessors and the fields, including the backing storage of an auto-accessor.
func (e *elementDef) appendElements(elems []storage.ClassElement) []storage.ClassElement {
	private := e.key.O != nil && e.key.O.Class == storage.ClassPrivateName
	switch e.kind {
	case bytecode.ElemMethod, bytecode.ElemGetter, bytecode.ElemSetter:
		if private {
			elems = append(elems, storage.ClassElement{
				Kind: e.kind,
				Key:  e.key,
				Val:  e.value,
			})
		}
	case bytecode.ElemAccessor:
		if private {
			elems = append(elems, storage.ClassElement{
				Kind: bytecode.ElemGetter,
				Key:  e.key,
				Val:  e.getter,
			}, storage.ClassElement{
				Kind: bytecode.ElemSetter,
				Key:  e.key,
				Val:  e.setter,
			})
		}
		elems = append(elems, storage.ClassElement{
			Kind: bytecode.ElemField,
			Key:  storage.Value{O: e.storage},
			Val:  e.value,
			Dec:  &e.dec,
		})
	case bytecode.ElemField:
		elems = append(elems, storage.ClassElement{
			Kind: bytecode.ElemField,
			Key:  e.key,
			Val:  e.value,
			Dec:  &e.dec,
		})
	}
	return elems
}

// defineMethodProperty implements DefineMethodProperty for the method, getter, setter or
// auto-accessor e of the class with the home object home.
func (cx *ExecutionContext) defineMethodProperty(home *storage.Object, e *elementDef) {
	if e.key.O != nil && e.key.O.Class == storage.ClassPrivateName {
		return
	}
	var d storage.PropDesc
	d.SetEnumerable(false)
	d.SetConfigurable(true)
	switch e.kind {
	case bytecode.ElemMethod:
		d = storage.DataDesc(storage.Value{O: e.value}, storage.FlagsHidden)
	case bytecode.ElemGetter:
		d.SetGetter(e.value)
	case bytecode.ElemSetter:
		d.SetSetter(e.value)
	case bytecode.ElemAccessor:
		d.SetGetter(e.getter)
		d.SetSetter(e.setter)
	}
	cx.DefinePropertyOrThrow(home, cx.ToPropertyKey(e.key), d)
}

// applyDecoratorsToElementDefinition implements ApplyDecoratorsToElementDefinition.
// The decorators of a field or an auto-accessor add their initializers to e.dec.
func (cx *ExecutionContext) applyDecoratorsToElementDefinition(e *elementDef, extra *[]*storage.Object, metadata *storage.Object) {
	// The decorator closest to the element applies first.
	for _, d := range slices.Backward(e.decorators) {
		state := &decorationState{}
		ctx := cx.createDecoratorContextObject(e.kind, e.key, extra, state, metadata, e.static, false)
		value := storage.Undefined
		switch e.kind {
		case bytecode.ElemMethod, bytecode.ElemGetter, bytecode.ElemSetter:
			value = storage.Value{O: e.value}
		case bytecode.ElemAccessor:
			o := cx.NewOrdinaryObject()
			cx.CreateDataPropertyOrThrow(o, storage.NamedKey(cx.Key(storage.KeyGet)), storage.Value{O: e.getter})
			cx.CreateDataPropertyOrThrow(o, storage.NamedKey(cx.Key(storage.KeySet)), storage.Value{O: e.setter})
			value = storage.Value{O: o}
		}
		nv := func() storage.Value {
			defer func() {
				state.finished = true
			}()
			return cx.CallValue(d.decorator, d.receiver, value, ctx)
		}()
		switch e.kind {
		case bytecode.ElemField:
			if nv.IsCallable() {
				e.dec.Initializers = append(e.dec.Initializers, nv.O)
			} else if !nv.IsUndefined() {
				cx.ThrowTypeError("a field decorator must return a function or undefined")
			}
		case bytecode.ElemAccessor:
			if nv.IsObject() {
				if g := cx.decoratorResultFunction(nv, cx.Key(storage.KeyGet)); g != nil {
					e.getter = g
				}
				if s := cx.decoratorResultFunction(nv, cx.Key(storage.KeySet)); s != nil {
					e.setter = s
				}
				if init := cx.decoratorResultFunction(nv, cx.Intern("init")); init != nil {
					e.dec.Initializers = append(e.dec.Initializers, init)
				}
			} else if !nv.IsUndefined() {
				cx.ThrowTypeError("an accessor decorator must return an object or undefined")
			}
		default:
			if nv.IsCallable() {
				e.value = nv.O
			} else if !nv.IsUndefined() {
				cx.ThrowTypeError("a method decorator must return a function or undefined")
			}
		}
	}
	e.decorators = nil
}

// decoratorResultFunction returns the property key of the object that an accessor decorator
// returned when it is a function, or nil when it is undefined.
func (cx *ExecutionContext) decoratorResultFunction(o storage.Value, key *storage.Object) *storage.Object {
	v := cx.Get(o.O, storage.NamedKey(key))
	if v.IsCallable() {
		return v.O
	}
	if !v.IsUndefined() {
		cx.ThrowTypeError("the " + wtf8.ToValidUTF8(key.Text) + " property that an accessor decorator returned must be a function or undefined")
	}
	return nil
}

// applyDecoratorsToClassDefinition implements ApplyDecoratorsToClassDefinition.
func (cx *ExecutionContext) applyDecoratorsToClassDefinition(st *classDecoration, classDef storage.Value) storage.Value {
	for _, d := range slices.Backward(st.classDecorators) {
		state := &decorationState{}
		ctx := cx.createDecoratorContextObject(0, st.name, &st.classInitializers, state, st.metadata, false, true)
		nd := func() storage.Value {
			defer func() {
				state.finished = true
			}()
			return cx.CallValue(d.decorator, d.receiver, classDef, ctx)
		}()
		if nd.IsCallable() {
			classDef = nd
		} else if !nd.IsUndefined() {
			cx.ThrowTypeError("a class decorator must return a function or undefined")
		}
	}
	st.classDecorators = nil
	return classDef
}

// finishClass performs the last steps of ClassDefinitionEvaluation for the class f: the
// initializers of the static methods, the static elements, and the initializers of the class.
func (cx *ExecutionContext) finishClass(st *classDecoration, f storage.Value) {
	for _, init := range st.staticMethodInitializers {
		cx.Call(init, f)
	}
	for _, e := range st.elements {
		if !e.static {
			continue
		}
		switch e.kind {
		case bytecode.ElemField, bytecode.ElemAccessor:
			for _, c := range e.appendElements(nil) {
				if c.Kind == bytecode.ElemField {
					cx.defineClassElement(f, &c)
				}
			}
		case bytecode.ElemStaticBlock:
			cx.Call(e.value, f)
		}
	}
	for _, init := range st.classInitializers {
		cx.Call(init, f)
	}
}

// createDecoratorContextObject implements CreateDecoratorContextObject. kind is a bytecode
// Elem kind, which isClass overrides.
func (cx *ExecutionContext) createDecoratorContextObject(kind int, name storage.Value, initializers *[]*storage.Object, state *decorationState, metadata *storage.Object, static, isClass bool) storage.Value {
	ctx := cx.NewOrdinaryObject()
	var kindStr wtf8.String
	switch {
	case isClass:
		kindStr = "class"
	case kind == bytecode.ElemMethod:
		kindStr = "method"
	case kind == bytecode.ElemGetter:
		kindStr = "getter"
	case kind == bytecode.ElemSetter:
		kindStr = "setter"
	case kind == bytecode.ElemAccessor:
		kindStr = "accessor"
	default:
		kindStr = "field"
	}
	def := func(key wtf8.String, v storage.Value) {
		cx.CreateDataPropertyOrThrow(ctx, storage.NamedKey(cx.Intern(key)), v)
	}
	def("kind", cx.NewString(kindStr))
	if !isClass {
		def("access", cx.createDecoratorAccessObject(kind, name))
		def("static", storage.Bool(static))
		if name.O != nil && name.O.Class == storage.ClassPrivateName {
			def("private", storage.True)
			def("name", cx.NewString("#"+name.O.Text))
		} else {
			def("private", storage.False)
			def("name", name)
		}
	} else {
		def("name", name)
	}
	def("addInitializer", storage.Value{O: cx.NewNativeWithState("addInitializer", 1, nativeAddInitializerFunction, &addInitializerState{
		initializers: initializers,
		decoration:   state,
	})})
	def("metadata", storage.Value{O: metadata})
	return storage.Value{O: ctx}
}

// addInitializerState is the state of an addInitializer function of a decorator context.
type addInitializerState struct {
	initializers *[]*storage.Object
	decoration   *decorationState
}

// addInitializerFunction is the code of the addInitializer functions. Their state is an
// *addInitializerState.
func addInitializerFunction(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	st := cx.ActiveNativeState().(*addInitializerState)
	if st.decoration.finished {
		cx.ThrowTypeError("addInitializer cannot be called after the decorator returned")
	}
	init := argAt(args, 0)
	if !init.IsCallable() {
		cx.ThrowTypeError("an initializer must be a function")
	}
	*st.initializers = append(*st.initializers, init.O)
	return storage.Undefined
}

// createDecoratorAccessObject implements CreateDecoratorAccessObject.
func (cx *ExecutionContext) createDecoratorAccessObject(kind int, name storage.Value) storage.Value {
	access := cx.NewOrdinaryObject()
	st := &decoratorAccess{
		name: name,
	}
	if kind != bytecode.ElemSetter {
		getter := cx.NewNativeWithState("", 1, nativeDecoratorAccessGetter, st)
		cx.CreateDataPropertyOrThrow(access, storage.NamedKey(cx.Key(storage.KeyGet)), storage.Value{O: getter})
	}
	if kind == bytecode.ElemField || kind == bytecode.ElemAccessor || kind == bytecode.ElemSetter {
		setter := cx.NewNativeWithState("", 2, nativeDecoratorAccessSetter, st)
		cx.CreateDataPropertyOrThrow(access, storage.NamedKey(cx.Key(storage.KeySet)), storage.Value{O: setter})
	}
	has := cx.NewNativeWithState("has", 1, nativeDecoratorAccessHas, st)
	cx.CreateDataPropertyOrThrow(access, storage.NamedKey(cx.Intern("has")), storage.Value{O: has})
	return storage.Value{O: access}
}

// decoratorAccess is the state of the functions of a decorator access object.
type decoratorAccess struct {
	// name is the property key or the private name of the decorated element.
	name storage.Value
}

// private reports whether the name is a private name.
func (a *decoratorAccess) private() bool {
	return a.name.O != nil && a.name.O.Class == storage.ClassPrivateName
}

// checkDecoratorAccessObject throws when o, which a decorator access function was called for, is
// not an object.
func (cx *ExecutionContext) checkDecoratorAccessObject(o storage.Value) {
	if !o.IsObject() {
		cx.ThrowTypeError("the object of a decorator access must be an object")
	}
}

// decoratorAccessGetter is the code of the get functions of decorator access objects. Their state
// is a *decoratorAccess.
func decoratorAccessGetter(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	a := cx.ActiveNativeState().(*decoratorAccess)
	o := argAt(args, 0)
	cx.checkDecoratorAccessObject(o)
	if a.private() {
		return cx.PrivateGet(o, a.name.O)
	}
	return cx.Get(o.O, cx.ToPropertyKey(a.name))
}

// decoratorAccessSetter is the code of the set functions of decorator access objects. Their state
// is a *decoratorAccess.
func decoratorAccessSetter(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	a := cx.ActiveNativeState().(*decoratorAccess)
	o := argAt(args, 0)
	cx.checkDecoratorAccessObject(o)
	if a.private() {
		cx.PrivateSet(o, a.name.O, argAt(args, 1))
	} else {
		cx.Set(o.O, cx.ToPropertyKey(a.name), argAt(args, 1))
	}
	return storage.Undefined
}

// decoratorAccessHas is the code of the has functions of decorator access objects. Their state is
// a *decoratorAccess.
func decoratorAccessHas(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	a := cx.ActiveNativeState().(*decoratorAccess)
	o := argAt(args, 0)
	cx.checkDecoratorAccessObject(o)
	if a.private() {
		_, ok := cx.privateStorage(o.O).Shape.Lookup(a.name.O)
		return storage.Bool(ok)
	}
	return storage.Bool(cx.HasProperty(o.O, cx.ToPropertyKey(a.name)))
}
