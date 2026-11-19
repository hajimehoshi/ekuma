// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/regexp"
)

// regExpState holds the internal slots of a RegExp object. It does not change once it is
// set, so objects can share it.
type regExpState struct {
	matcher *regexp.Regexp
	source  string
	flags   string
	legacy  bool
	// lent marks the state of a RegExp object that [VM.LendRegExpLiterals] lends.
	lent bool

	// realm is the [[Realm]] of the object.
	realm *realm
}

// regExpSite is a regular expression literal of a function.
type regExpSite struct {
	literal bytecode.RegExp

	// state is the internal state that the RegExp objects of the literal share, once the
	// pattern is compiled.
	state *regExpState

	// lent is the RegExp object that [VM.LendRegExpLiterals] lends for the literal, once made.
	lent *Object
}

// regExpCacheLimit bounds the number of compiled patterns that a [VM] keeps.
const regExpCacheLimit = 1024

// RegExpAlloc implements RegExpAlloc: it returns a RegExp object for the constructor
// newTarget, with an own lastIndex property and no matcher yet.
func (vm *VM) RegExpAlloc(newTarget *Object) *Object {
	legacy := newTarget == vm.Intrinsic(IntrinsicRegExp)
	var s *shape
	if legacy {
		// The prototype property of %RegExp% is neither writable nor configurable.
		s = vm.regExpShape()
	} else {
		proto := vm.GetPrototypeFromConstructor(newTarget, vm.Intrinsic(IntrinsicRegExpPrototype))
		s = vm.shapeFor(proto).with(vm.Key(keyLastIndex), FlagWritable)
	}
	return vm.newRegExpObject(s, &regExpState{
		legacy: legacy,
		realm:  vm.curRealm,
	})
}

// regExpShape returns the shape of a new RegExp object whose prototype is %RegExp.prototype%.
func (vm *VM) regExpShape() *shape {
	proto := vm.Intrinsic(IntrinsicRegExpPrototype)
	if s := vm.shapes.regExp; s != nil && s.proto == proto {
		return s
	}
	s := vm.shapeFor(proto).with(vm.Key(keyLastIndex), FlagWritable)
	vm.shapes.regExp = s
	return s
}

// newRegExpObject returns a new RegExp object with the shape s, whose only property is
// lastIndex, and the internal state st.
func (vm *VM) newRegExpObject(s *shape, st *regExpState) *Object {
	o, slots := newObjectWithStorage(1)
	o.kind = kindObject
	o.class = classRegExp
	o.shape = s
	o.slots = slots
	o.slots[0] = Number(0)
	o.ext = st
	return o
}

// RegExpInitialize implements RegExpInitialize for the RegExp object o and returns o.
// It throws a SyntaxError for an invalid pattern or invalid flags.
func (vm *VM) RegExpInitialize(o *Object, pattern, flags Value) *Object {
	p := ""
	if !pattern.IsUndefined() {
		p = vm.ToString(pattern)
	}
	f := ""
	if !flags.IsUndefined() {
		f = vm.ToString(flags)
	}
	re := vm.compileRegExp(p, f)
	o.ext = &regExpState{
		matcher: re,
		source:  p,
		flags:   f,
		legacy:  o.ext.(*regExpState).legacy,
		realm:   o.ext.(*regExpState).realm,
	}
	vm.Set(o, NamedKey(vm.Key(keyLastIndex)), Number(0))
	return o
}

// compileRegExp compiles the pattern p with the flags f, or throws a SyntaxError.
func (vm *VM) compileRegExp(p, f string) *regexp.Regexp {
	key := f + "/" + p
	if re, ok := vm.regExps[key]; ok {
		return re
	}
	flags, err := regexp.ParseFlags(f)
	if err != nil {
		vm.throwSyntaxError(err.Error())
	}
	re, err := regexp.Compile(p, flags, vm)
	if err != nil {
		vm.throwSyntaxError(err.Error())
	}
	if vm.regExps == nil || len(vm.regExps) >= regExpCacheLimit {
		vm.regExps = map[string]*regexp.Regexp{}
	}
	vm.regExps[key] = re
	return re
}

// newRegExpLiteral evaluates the regular expression literal site: it returns a new RegExp object.
func (vm *VM) newRegExpLiteral(site *regExpSite) *Object {
	return vm.newRegExpObject(vm.regExpShape(), vm.regExpLiteralState(site))
}

// regExpLiteralState returns the state that the RegExp objects of the literal site share.
func (vm *VM) regExpLiteralState(site *regExpSite) *regExpState {
	if site.state == nil {
		site.state = &regExpState{
			matcher: vm.compileRegExp(site.literal.Pattern, site.literal.Flags),
			source:  site.literal.Pattern,
			flags:   site.literal.Flags,
			legacy:  true,
			realm:   vm.curRealm,
		}
	}
	return site.state
}

// LendRegExpLiterals marks the native function f as one that may receive, as the first argument
// of a call in the current realm written with a regular expression literal there, a RegExp object
// that other evaluations of the literal reuse instead of a new one. Unless f replaces that
// argument by [VM.OwnRegExp] before using it, f must not keep it, expose it to script code, or use
// it after running script code.
func (vm *VM) LendRegExpLiterals(f *Object) {
	vm.regExpBorrowers = append(vm.regExpBorrowers, f)
}

// LendRegExpLiteralReceivers is like [VM.LendRegExpLiterals] for the this value of a method call
// on a regular expression literal.
func (vm *VM) LendRegExpLiteralReceivers(f *Object) {
	vm.regExpReceiverBorrowers = append(vm.regExpReceiverBorrowers, f)
}

// lendRegExpLiteral evaluates the regular expression literal site for the call whose callee,
// this value and arguments are in call: it sets the first argument, or the this value when
// receiver is true and [VM.regExpLiteralMethod] left the lent object there.
func (vm *VM) lendRegExpLiteral(call []Value, site *regExpSite, receiver bool) {
	if !receiver {
		call[2] = Value{o: vm.regExpLiteralFor(call[0].o, site, vm.regExpBorrowers)}
		return
	}
	if o := call[1].o; o != nil && o == site.lent {
		call[1] = Value{o: vm.regExpLiteralFor(call[0].o, site, vm.regExpReceiverBorrowers)}
	}
}

// regExpLiteralFor returns the lent object of the literal site, as new, when f is one of
// borrowers and [VM.IsUnmodifiedRegExp] holds for it, or else a new RegExp object.
func (vm *VM) regExpLiteralFor(f *Object, site *regExpSite, borrowers []*Object) *Object {
	if f == nil || !slices.Contains(borrowers, f) {
		return vm.newRegExpLiteral(site)
	}
	o := vm.resetLentRegExp(site)
	if !vm.IsUnmodifiedRegExp(o) {
		return vm.newRegExpLiteral(site)
	}
	return o
}

// resetLentRegExp returns the lent object of the literal site in the state of a new RegExp object.
func (vm *VM) resetLentRegExp(site *regExpSite) *Object {
	o := site.lent
	if o == nil {
		lent := *vm.regExpLiteralState(site)
		lent.lent = true
		o = vm.newRegExpObject(vm.regExpShape(), &lent)
		site.lent = o
		return o
	}
	// A borrower that runs script code may still hold o, but no longer uses it.
	o.shape = vm.regExpShape()
	o.slots[0] = Number(0)
	return o
}

// regExpLiteralMethod reads the method key of a RegExp object for the literal site, using the
// cache c, and returns it with the object to call it on. When reading it runs no code, that
// object is the lent object of the literal, which [VM.lendRegExpLiteral] replaces before the call.
func (vm *VM) regExpLiteralMethod(site *regExpSite, key *Object, c *propCache) (method Value, this *Object) {
	o := vm.resetLentRegExp(site)
	e := &c.cacheEntry
	if o.shape != e.shape {
		e = &c.second
	}
	if o.shape == e.shape && e.kind == cacheProto && e.epoch == vm.protoEpoch {
		return e.holder.slots[e.slot], o
	}
	if _, ok := vm.GetVWithoutCall(Value{o: o}, key); ok {
		v, _ := vm.getNamed(Value{o: o}, key, c)
		return v, o
	}
	o = vm.newRegExpLiteral(site)
	return vm.GetV(Value{o: o}, key), o
}

// OwnRegExp returns v, or a new RegExp object for the same literal when v is a RegExp object
// lent by [VM.LendRegExpLiterals]. v must not have been used since it was lent.
func (vm *VM) OwnRegExp(v Value) Value {
	if o := v.o; o != nil && o.class == classRegExp {
		if st := o.ext.(*regExpState); st.lent {
			own := *st
			own.lent = false
			return Value{o: vm.newRegExpObject(vm.regExpShape(), &own)}
		}
	}
	return v
}

// RegExpMatcher returns the compiled pattern of the RegExp object o,
// or nil when o is not a RegExp object.
func (o *Object) RegExpMatcher() *regexp.Regexp {
	if o.class != classRegExp {
		return nil
	}
	return o.ext.(*regExpState).matcher
}

// RegExpOriginal returns the original source and flags of the RegExp object o.
func (o *Object) RegExpOriginal() (source, flags string) {
	st := o.ext.(*regExpState)
	return st.source, st.flags
}

// RegExpLegacyFeaturesEnabled implements the [[LegacyFeaturesEnabled]] slot of the RegExp
// object o: whether o was created for the RegExp constructor itself.
func (o *Object) RegExpLegacyFeaturesEnabled() bool {
	return o.ext.(*regExpState).legacy
}

// IsRegExpOfCurrentRealm reports whether the [[Realm]] of the RegExp object o is the current realm.
func (vm *VM) IsRegExpOfCurrentRealm(o *Object) bool {
	return o.ext.(*regExpState).realm == vm.curRealm
}

// regExpOriginals is what [VM.IsUnmodifiedRegExp] compares against.
type regExpOriginals struct {
	props []originalProp

	// epoch is the value of VM.protoEpoch when the slots of props were last found.
	epoch   uint32
	checked bool
	// found reports whether every property of props had its recorded attributes at epoch,
	// and every accessor property its recorded getter.
	found bool
}

// originalProp is a recorded own property of an object.
type originalProp struct {
	holder *Object
	key    *Object
	flags  uint8
	// value is the value of a data property, or the getter of an accessor property.
	value *Object
	// slot is the slot of the property in holder, found at regExpOriginals.epoch.
	slot int32
}

// find looks the property up in its holder, and reports whether it has the recorded attributes,
// and the recorded getter if it is an accessor property.
func (p *originalProp) find() bool {
	q, ok := p.holder.shape.lookup(p.key)
	if !ok || q.slot < 0 || q.flags != p.flags {
		return false
	}
	p.slot = q.slot
	if q.flags&FlagAccessor != 0 {
		return p.holder.slots[q.slot].accessor().getter == p.value
	}
	return true
}

// RecordRegExpOriginals records the current own properties protoKeys of %RegExp.prototype%
// and ctorKeys of %RegExp%, which [VM.IsUnmodifiedRegExp] requires to stay unchanged.
// Every key must name an existing property whose value, if it is a data property, is an object.
func (vm *VM) RecordRegExpOriginals(protoKeys, ctorKeys []*Object) {
	proto := vm.Intrinsic(IntrinsicRegExpPrototype)
	ctor := vm.Intrinsic(IntrinsicRegExp)
	r := &vm.regExpOriginals
	// hasRegExpShape compares against this shape, so it must exist from now on.
	vm.regExpShape()
	r.props = r.props[:0]
	record := func(holder *Object, keys []*Object) {
		for _, k := range keys {
			p, ok := holder.shape.lookup(k)
			if !ok || p.slot < 0 {
				panic("engine: RecordRegExpOriginals: missing property " + k.str)
			}
			v := holder.slots[p.slot]
			var val *Object
			if p.flags&FlagAccessor != 0 {
				val = v.accessor().getter
			} else {
				val = v.o
			}
			if val == nil {
				panic("engine: RecordRegExpOriginals: property " + k.str + " holds no object")
			}
			r.props = append(r.props, originalProp{
				holder: holder,
				key:    k,
				flags:  p.flags,
				value:  val,
				slot:   p.slot,
			})
		}
	}
	record(proto, protoKeys)
	record(ctor, ctorKeys)
	// Adding, removing or redefining a property of an object marked as a prototype moves
	// protoEpoch. Assigning to a data property does not.
	ctor.flags |= objIsProto
	r.checked = false
}

// IsUnmodifiedRegExp reports whether o is an initialized RegExp object whose prototype is
// %RegExp.prototype% and whose only own property is a writable lastIndex, while the properties
// recorded by [VM.RecordRegExpOriginals] are unchanged. Then those properties are the only
// ones that reading the recorded keys from o finds, and lastIndex is an ordinary data property.
func (vm *VM) IsUnmodifiedRegExp(o *Object) bool {
	if o.class != classRegExp || !vm.hasRegExpShape(o) || o.ext.(*regExpState).matcher == nil {
		return false
	}
	r := &vm.regExpOriginals
	if !r.checked || r.epoch != vm.protoEpoch {
		r.found = true
		for i := range r.props {
			if !r.props[i].find() {
				r.found = false
				break
			}
		}
		r.epoch = vm.protoEpoch
		r.checked = true
	}
	if !r.found {
		return false
	}
	for i := range r.props {
		p := &r.props[i]
		if p.flags&FlagAccessor == 0 && p.holder.slots[p.slot].o != p.value {
			return false
		}
	}
	return true
}

// hasRegExpShape reports whether o has the shape of a new RegExp object whose prototype is
// %RegExp.prototype%, so that its only own property is a writable lastIndex in slot 0.
func (vm *VM) hasRegExpShape(o *Object) bool {
	s := vm.shapes.regExp
	return s != nil && o.shape == s
}

// RegExpLastIndex returns the value of the own lastIndex property of o, or false when o may not
// have it as a writable data property where [VM.RegExpAlloc] puts it.
func (vm *VM) RegExpLastIndex(o *Object) (Value, bool) {
	if !vm.hasRegExpShape(o) {
		return Undefined, false
	}
	return o.slots[0], true
}

// SetRegExpLastIndex sets the own lastIndex property of o to v and reports true, or reports
// false when o may not have it as a writable data property where [VM.RegExpAlloc] puts it.
func (vm *VM) SetRegExpLastIndex(o *Object, v Value) bool {
	if !vm.hasRegExpShape(o) {
		return false
	}
	o.slots[0] = v
	vm.touchProto(o)
	return true
}

// matchResultShapes are the shapes that a match result array has after
// [VM.DefineMatchResultProperties] or [VM.DefineMatchResultPropertiesWithIndices].
type matchResultShapes struct {
	// array is the shape of a new array that the others derive from.
	array *shape

	// plain has index, input and groups, and withIndices also indices.
	plain, withIndices *shape
}

// matchResultShapesFor returns the match result shapes that derive from the shape array of a
// new array of the current realm.
func (vm *VM) matchResultShapesFor(array *shape) *matchResultShapes {
	m := &vm.matchResultShapes
	if m.array != array {
		plain := array.with(vm.Key(keyIndex), FlagsData).with(vm.Key(keyInput), FlagsData).with(vm.Key(keyGroups), FlagsData)
		*m = matchResultShapes{
			array:       array,
			plain:       plain,
			withIndices: plain.with(vm.Intern("indices"), FlagsData),
		}
	}
	return m
}

// DefineMatchResultProperties creates the data properties index, input and groups of a match
// result on a, in that order, as CreateDataPropertyOrThrow does. a must be an array from
// [VM.NewArrayWithRoom] that has no named properties.
func (vm *VM) DefineMatchResultProperties(a *Object, index, input, groups Value) {
	if a.shape != vm.shapes.array || len(a.slots) != 0 {
		panic("engine: DefineMatchResultProperties on an array that is not new")
	}
	a.shape = vm.matchResultShapesFor(a.shape).plain
	a.slots = append(a.slots, index, input, groups)
}

// DefineMatchResultPropertiesWithIndices is like [VM.DefineMatchResultProperties] and creates
// the data property indices after groups.
func (vm *VM) DefineMatchResultPropertiesWithIndices(a *Object, index, input, groups, indices Value) {
	if a.shape != vm.shapes.array || len(a.slots) != 0 {
		panic("engine: DefineMatchResultPropertiesWithIndices on an array that is not new")
	}
	a.shape = vm.matchResultShapesFor(a.shape).withIndices
	a.slots = append(a.slots, index, input, groups, indices)
}
