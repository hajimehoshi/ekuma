// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// regExpState holds the internal slots of a RegExp object. It does not change once it is
// set, so objects can share it.
type regExpState struct {
	matcher *regexp.Regexp
	source  wtf8.String
	flags   wtf8.String
	legacy  bool
	// lent marks the state of a RegExp object that [ExecutionContext.LendRegExpLiterals] lends.
	lent bool

	// realm is the [[Realm]] of the object.
	realm *realmPart
}

// regExpSite is a regular expression literal of a function.
type regExpSite struct {
	literal bytecode.RegExp

	// state is the internal state that the RegExp objects of the literal share, once the
	// pattern is compiled.
	state *regExpState

	// lent is the RegExp object that [ExecutionContext.LendRegExpLiterals] lends for the literal, once made.
	lent *Object
}

// regExpCacheLimit bounds the number of compiled patterns that an agent keeps.
const regExpCacheLimit = 1024

// RegExpAlloc implements RegExpAlloc: it returns a RegExp object for the constructor
// newTarget, with an own lastIndex property and no matcher yet.
func (cx *ExecutionContext) RegExpAlloc(newTarget *Object) *Object {
	legacy := newTarget == cx.Intrinsic(IntrinsicRegExp)
	var s *shape
	if legacy {
		// The prototype property of %RegExp% is neither writable nor configurable.
		s = cx.regExpShape()
	} else {
		proto := cx.GetPrototypeFromConstructor(newTarget, IntrinsicRegExpPrototype)
		s = cx.shapeFor(proto).with(cx.Key(KeyLastIndex), FlagWritable)
	}
	return cx.newRegExpObject(s, &regExpState{
		legacy: legacy,
		realm:  cx.realmPart,
	})
}

// regExpShape returns the shape of a new RegExp object whose prototype is %RegExp.prototype%.
func (cx *ExecutionContext) regExpShape() *shape {
	proto := cx.Intrinsic(IntrinsicRegExpPrototype)
	if s := cx.realm.shapes.regExp; s != nil && s.proto == proto {
		return s
	}
	s := cx.shapeFor(proto).with(cx.Key(KeyLastIndex), FlagWritable)
	cx.realm.shapes.regExp = s
	return s
}

// newRegExpObject returns a new RegExp object with the shape s, whose only property is
// lastIndex, and the internal state st.
func (ag *agent) newRegExpObject(s *shape, st *regExpState) *Object {
	o, slots := newObjectWithStorage(1)
	o.kind = kindObject
	o.class = classRegExp
	o.shape = s
	o.initSlots(slots)
	o.slots[0] = Number(0)
	o.setExtState(st)
	return o
}

// RegExpInitialize implements RegExpInitialize for the RegExp object o and returns o.
// It throws a SyntaxError for an invalid pattern or invalid flags.
func (cx *ExecutionContext) RegExpInitialize(o *Object, pattern, flags Value) *Object {
	var r bytecode.RegExp
	if !pattern.IsUndefined() {
		r.Pattern = cx.ToString(pattern)
	}
	if !flags.IsUndefined() {
		r.Flags = cx.ToString(flags)
	}
	re := cx.compileRegExp(r)
	o.setExtState(&regExpState{
		matcher: re,
		source:  r.Pattern,
		flags:   r.Flags,
		legacy:  o.extState().(*regExpState).legacy,
		realm:   o.extState().(*regExpState).realm,
	})
	cx.Set(o, NamedKey(cx.Key(KeyLastIndex)), Number(0))
	return o
}

// compileRegExp compiles r or throws a SyntaxError.
func (cx *ExecutionContext) compileRegExp(r bytecode.RegExp) *regexp.Regexp {
	if re, ok := cx.regExps[r]; ok {
		return re
	}
	flags, err := regexp.ParseFlags(r.Flags)
	if err != nil {
		cx.ThrowSyntaxError(err.Error())
	}
	re, err := regexp.Compile(r.Pattern, flags, cx)
	if err != nil {
		cx.ThrowSyntaxError(err.Error())
	}
	if cx.regExps == nil || len(cx.regExps) >= regExpCacheLimit {
		cx.regExps = map[bytecode.RegExp]*regexp.Regexp{}
	}
	cx.regExps[r] = re
	return re
}

// newRegExpLiteral evaluates the regular expression literal site: it returns a new RegExp object.
func (cx *ExecutionContext) newRegExpLiteral(site *regExpSite) *Object {
	return cx.newRegExpObject(cx.regExpShape(), cx.regExpLiteralState(site))
}

// regExpLiteralState returns the state that the RegExp objects of the literal site share.
func (cx *ExecutionContext) regExpLiteralState(site *regExpSite) *regExpState {
	if site.state == nil {
		site.state = &regExpState{
			matcher: cx.compileRegExp(site.literal),
			source:  site.literal.Pattern,
			flags:   site.literal.Flags,
			legacy:  true,
			realm:   cx.realmPart,
		}
	}
	return site.state
}

// LendRegExpLiterals marks the native function f as one that may receive, as the first argument
// of a call in the current realm written with a regular expression literal there, a RegExp object
// that other evaluations of the literal reuse instead of a new one. Unless f replaces that
// argument by [ExecutionContext.OwnRegExp] before using it, f must not keep it, expose it to script code, or use
// it after running script code.
func (cx *ExecutionContext) LendRegExpLiterals(f *Object) {
	cx.realm.regExpBorrowers = append(cx.realm.regExpBorrowers, f)
}

// LendRegExpLiteralReceivers is like [ExecutionContext.LendRegExpLiterals] for the this value of a method call
// on a regular expression literal.
func (cx *ExecutionContext) LendRegExpLiteralReceivers(f *Object) {
	cx.realm.regExpReceiverBorrowers = append(cx.realm.regExpReceiverBorrowers, f)
}

// lendRegExpLiteral evaluates the regular expression literal site into the register dst of
// regs for the call of the callee in the register callee: dst is the first argument, or the
// this value, which it sets only when [ExecutionContext.regExpLiteralMethod] left the lent object there.
func (cx *ExecutionContext) lendRegExpLiteral(regs []Value, dst, callee int32, site *regExpSite) {
	f := regs[callee].o
	if dst == callee+2 {
		regs[dst] = Value{o: cx.regExpLiteralFor(f, site, cx.realm.regExpBorrowers)}
		return
	}
	if o := regs[dst].o; o != nil && o == site.lent {
		// The register keeps the lent object, without a write barrier, when f borrows it.
		if lent := cx.regExpLiteralFor(f, site, cx.realm.regExpReceiverBorrowers); lent != o {
			regs[dst] = Value{o: lent}
		}
	}
}

// regExpLiteralFor returns the lent object of the literal site, as new, when f is one of
// borrowers and [QuietContext.UnmodifiedRegExpMatcher] accepts it, or else a new RegExp object.
func (cx *ExecutionContext) regExpLiteralFor(f *Object, site *regExpSite, borrowers []*Object) *Object {
	if f == nil || !slices.Contains(borrowers, f) {
		return cx.newRegExpLiteral(site)
	}
	o := cx.resetLentRegExp(site)
	if cx.Quiet().UnmodifiedRegExpMatcher(o) == nil {
		return cx.newRegExpLiteral(site)
	}
	return o
}

// resetLentRegExp returns the lent object of the literal site in the state of a new RegExp object.
func (cx *ExecutionContext) resetLentRegExp(site *regExpSite) *Object {
	o := site.lent
	if o == nil {
		lent := *cx.regExpLiteralState(site)
		lent.lent = true
		o = cx.newRegExpObject(cx.regExpShape(), &lent)
		site.lent = o
		return o
	}
	// A borrower that runs script code may still hold o, but no longer uses it. Only the pointers
	// that differ from those of a new object are stored, so that reusing o needs no write barrier
	// while the garbage collector marks.
	if s := cx.regExpShape(); o.shape != s {
		o.shape = s
	}
	setNumber(&o.slots[0], 0)
	return o
}

// regExpLiteralMethod reads the method key of a RegExp object for the literal site, using the
// cache c, and returns it with the object to call it on. When reading it runs no code, that
// object is the lent object of the literal, which [ExecutionContext.lendRegExpLiteral] replaces before the call.
func (cx *ExecutionContext) regExpLiteralMethod(site *regExpSite, key *Object, c *propCache) (method Value, this *Object) {
	o := cx.resetLentRegExp(site)
	e := &c.cacheEntry
	if o.shape != e.shape {
		e = &c.second
	}
	if o.shape == e.shape && e.kind == cacheProto && e.epoch == cx.protoEpoch {
		if validateExec {
			cx.checkEntryRead(nil, 0, Value{o: o}, o, key, e)
		}
		return *e.ref, o
	}
	if v, ok := cx.Quiet().getVWithoutCall(Value{o: o}, key, c); ok {
		return v, o
	}
	o = cx.newRegExpLiteral(site)
	return cx.GetV(Value{o: o}, key), o
}

// OwnRegExp returns v, or a new RegExp object for the same literal when v is a RegExp object
// lent by [ExecutionContext.LendRegExpLiterals]. v must not have been used since it was lent.
func (cx *ExecutionContext) OwnRegExp(v Value) Value {
	if o := v.o; o != nil && o.class == classRegExp {
		if st := o.extState().(*regExpState); st.lent {
			own := *st
			own.lent = false
			return Value{o: cx.newRegExpObject(cx.regExpShape(), &own)}
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
	return o.extState().(*regExpState).matcher
}

// RegExpOriginal returns the original source and flags of the RegExp object o.
func (o *Object) RegExpOriginal() (source, flags wtf8.String) {
	st := o.extState().(*regExpState)
	return st.source, st.flags
}

// RegExpLegacyFeaturesEnabled implements the [[LegacyFeaturesEnabled]] slot of the RegExp
// object o: whether o was created for the RegExp constructor itself.
func (o *Object) RegExpLegacyFeaturesEnabled() bool {
	return o.extState().(*regExpState).legacy
}

// IsRegExpOfCurrentRealm reports whether the [[Realm]] of the RegExp object o is the current realm.
func (cx *ExecutionContext) IsRegExpOfCurrentRealm(o *Object) bool {
	return o.extState().(*regExpState).realm == cx.realmPart
}

// MatchRegExp matches re against the string s at the code unit index start, or after it unless re
// is sticky. It returns the code unit indices of the captures, or nil when there is no match. They
// are valid until the next match.
func (q QuietContext) MatchRegExp(re *regexp.Regexp, s Value, start int) []int {
	m := &q.cx.regExpMatcher
	str := s.Str()
	m.SetASCII(str, s.IsASCII())
	return re.Match(m, str, start, q.cx)
}

// FindRegExp is like [QuietContext.MatchRegExp], but it finds the first match at start or after it
// even when re is sticky.
func (q QuietContext) FindRegExp(re *regexp.Regexp, s Value, start int) []int {
	m := &q.cx.regExpMatcher
	str := s.Str()
	m.SetASCII(str, s.IsASCII())
	return re.Find(m, str, start, q.cx)
}

// RegExpMatchSpan returns the positions in the subject of the capture i of the last match, which
// must have succeeded.
func (q QuietContext) RegExpMatchSpan(i int) (start, end wtf8.Pos) {
	return q.cx.regExpMatcher.Span(i)
}

// RegExpFlagsString returns the string of the flags f in the order of the flags getter.
func (cx *ExecutionContext) RegExpFlagsString(f regexp.Flags) Value {
	t := cx.realm.regExpFlagStrings
	if t == nil {
		t = new([1 << 8]Value)
		cx.realm.regExpFlagStrings = t
	}
	v := &t[f]
	if !v.IsString() {
		*v = cx.NewString(wtf8.String(f.String()))
	}
	return *v
}

// legacyRegExpStatics holds the internal slots of %RegExp% for its legacy static properties.
type legacyRegExpStatics struct {
	// input and str are undefined when their respective properties are unavailable.
	input Value
	// str and caps are the subject and the capture indices of the last match, if any.
	str  Value
	caps []int
}

// RecordRegExpMatch updates the legacy static properties of %RegExp% after a match of the RegExp
// object r against s with the capture indices caps. It does nothing when the [[Realm]] of r is not
// the current realm.
func (q QuietContext) RecordRegExpMatch(r *Object, s Value, caps []int) {
	st := r.extState().(*regExpState)
	if st.realm != q.cx.realmPart {
		return
	}
	if st.legacy {
		q.UpdateLegacyRegExpStaticProperties(s, caps)
		return
	}
	// This is InvalidateLegacyRegExpStaticProperties.
	l := &q.cx.realm.legacyRegExp
	l.input = Undefined
	l.str = Undefined
	l.caps = l.caps[:0]
}

// UpdateLegacyRegExpStaticProperties implements UpdateLegacyRegExpStaticProperties for %RegExp%
// after a match against s with the capture indices caps.
func (q QuietContext) UpdateLegacyRegExpStaticProperties(s Value, caps []int) {
	l := &q.cx.realm.legacyRegExp
	l.input = s
	l.str = s
	l.caps = append(l.caps[:0], caps...)
}

// LegacyRegExpInput returns the [[RegExpInput]] slot of %RegExp%, or undefined for empty.
func (cx *ExecutionContext) LegacyRegExpInput() Value {
	return cx.realm.legacyRegExp.input
}

// SetLegacyRegExpInput sets the [[RegExpInput]] slot of %RegExp% to the string s.
func (cx *ExecutionContext) SetLegacyRegExpInput(s Value) {
	cx.realm.legacyRegExp.input = s
}

// LegacyRegExpMatch returns the subject and the capture indices of the last match that the legacy
// static properties of %RegExp% show. s is undefined when they are unavailable. Before the first
// match, s is the empty string and caps is empty. caps is valid until the next match.
func (cx *ExecutionContext) LegacyRegExpMatch() (s Value, caps []int) {
	l := &cx.realm.legacyRegExp
	return l.str, l.caps
}

// regExpOriginals is what [QuietContext.UnmodifiedRegExpMatcher] compares against.
type regExpOriginals struct {
	props []originalProp

	// data holds the data properties of props, found at epoch. Assigning to a data property
	// does not move protoEpoch, so their values are compared at every check.
	data []originalData

	// epoch is the value of agent.protoEpoch when the slots of props were last found.
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

// originalData is a recorded data property of an object.
type originalData struct {
	// ref points at the value of the property, which is in the slots of its holder.
	ref   *Value
	value *Object
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
// and ctorKeys of %RegExp%, which [QuietContext.UnmodifiedRegExpMatcher] requires to stay unchanged.
// Every key must name an existing property whose value, if it is a data property, is an object.
func (cx *ExecutionContext) RecordRegExpOriginals(protoKeys, ctorKeys []*Object) {
	proto := cx.Intrinsic(IntrinsicRegExpPrototype)
	ctor := cx.Intrinsic(IntrinsicRegExp)
	r := &cx.realm.regExpOriginals
	// hasRegExpShape compares against this shape, so it must exist from now on.
	cx.regExpShape()
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

// UnmodifiedRegExpMatcher returns the compiled pattern of o when o is an initialized RegExp object
// whose prototype is %RegExp.prototype% and whose only own property is a writable lastIndex, while
// the properties recorded by [ExecutionContext.RecordRegExpOriginals] are unchanged, and nil
// otherwise. When it is not nil, those properties are the only ones that reading the recorded keys
// from o finds, and lastIndex is an ordinary data property.
func (q QuietContext) UnmodifiedRegExpMatcher(o *Object) *regexp.Regexp {
	cx := q.cx
	if o.class != classRegExp || !cx.hasRegExpShape(o) {
		return nil
	}
	matcher := o.extState().(*regExpState).matcher
	if matcher == nil {
		return nil
	}
	r := &cx.realm.regExpOriginals
	if !r.checked || r.epoch != cx.protoEpoch {
		r.found = true
		r.data = r.data[:0]
		for i := range r.props {
			p := &r.props[i]
			if !p.find() {
				r.found = false
				break
			}
			if p.flags&FlagAccessor == 0 {
				r.data = append(r.data, originalData{
					ref:   &p.holder.slots[p.slot],
					value: p.value,
				})
			}
		}
		r.epoch = cx.protoEpoch
		r.checked = true
	}
	if !r.found {
		return nil
	}
	for _, d := range r.data {
		if d.ref.o != d.value {
			return nil
		}
	}
	if validateExec {
		cx.checkUnmodifiedRegExp(o)
	}
	return matcher
}

// hasRegExpShape reports whether o has the shape of a new RegExp object whose prototype is
// %RegExp.prototype%, so that its only own property is a writable lastIndex in slot 0.
func (r *realmPart) hasRegExpShape(o *Object) bool {
	if validateExec {
		r.checkRegExpShape(o)
	}
	s := r.shapes.regExp
	return s != nil && o.shape == s
}

// RegExpLastIndex returns the value of the own lastIndex property of o, or false when o may not
// have it as a writable data property where [ExecutionContext.RegExpAlloc] puts it.
func (q QuietContext) RegExpLastIndex(o *Object) (Value, bool) {
	if !q.cx.hasRegExpShape(o) {
		return Undefined, false
	}
	return o.slots[0], true
}

// SetRegExpLastIndex sets the own lastIndex property of o to v and reports true, or reports
// false when o may not have it as a writable data property where [ExecutionContext.RegExpAlloc] puts it.
func (q QuietContext) SetRegExpLastIndex(o *Object, v Value) bool {
	if !q.cx.hasRegExpShape(o) {
		return false
	}
	o.slots[0] = v
	q.cx.touchProto(o)
	return true
}

// matchResultShapes are the shapes that a match result array has after
// [ExecutionContext.DefineMatchResultProperties] or [ExecutionContext.DefineMatchResultPropertiesWithIndices].
type matchResultShapes struct {
	// array is the shape of a new array that the others derive from.
	array *shape

	// plain has index, input and groups, and withIndices also indices.
	plain, withIndices *shape
}

// matchResultShapesFor returns the match result shapes that derive from the shape array of a
// new array of the current realm.
func (cx *ExecutionContext) matchResultShapesFor(array *shape) *matchResultShapes {
	m := &cx.matchResultShapes
	if m.array != array {
		plain := array.with(cx.Key(KeyIndex), FlagsData).with(cx.Key(keyInput), FlagsData).with(cx.Key(KeyGroups), FlagsData)
		*m = matchResultShapes{
			array:       array,
			plain:       plain,
			withIndices: plain.with(cx.Intern("indices"), FlagsData),
		}
	}
	return m
}

// DefineMatchResultProperties creates the data properties index, input and groups of a match
// result on a, in that order, as CreateDataPropertyOrThrow does. a must be an array from
// [ExecutionContext.NewArrayWithRoom] that has no named properties.
func (cx *ExecutionContext) DefineMatchResultProperties(a *Object, index, input, groups Value) {
	if a.shape != cx.realm.shapes.array || len(a.slots) != 0 {
		panic("engine: DefineMatchResultProperties on an array that is not new")
	}
	a.shape = cx.matchResultShapesFor(a.shape).plain
	a.slots = append(a.slots, index, input, groups)
}

// DefineMatchResultPropertiesWithIndices is like [ExecutionContext.DefineMatchResultProperties] and creates
// the data property indices after groups.
func (cx *ExecutionContext) DefineMatchResultPropertiesWithIndices(a *Object, index, input, groups, indices Value) {
	if a.shape != cx.realm.shapes.array || len(a.slots) != 0 {
		panic("engine: DefineMatchResultPropertiesWithIndices on an array that is not new")
	}
	a.shape = cx.matchResultShapesFor(a.shape).withIndices
	a.slots = append(a.slots, index, input, groups, indices)
}
