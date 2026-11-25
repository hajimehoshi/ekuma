// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// regExpCacheLimit bounds the number of compiled patterns that an agent keeps.
const regExpCacheLimit = 1024

// RegExpAlloc implements RegExpAlloc: it returns a RegExp object for the constructor
// newTarget, with an own lastIndex property and no matcher yet.
func (cx *ExecutionContext) RegExpAlloc(newTarget *Object) *Object {
	legacy := newTarget == cx.Intrinsic(IntrinsicRegExp)
	var s *shape
	if legacy {
		// The prototype property of %RegExp% is neither writable nor configurable.
		s = cx.RegExpShape()
	} else {
		proto := cx.GetPrototypeFromConstructor(newTarget, IntrinsicRegExpPrototype)
		s = cx.ShapeFor(proto).With(cx.Key(KeyLastIndex), FlagWritable)
	}
	return storage.NewRegExpObject(s, &regExpState{
		Legacy: legacy,
		Realm:  cx.realmPart,
	})
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
	o.SetExtState(&regExpState{
		Matcher: re,
		Source:  r.Pattern,
		Flags:   r.Flags,
		Legacy:  o.ExtState().(*regExpState).Legacy,
		Realm:   o.ExtState().(*regExpState).Realm,
	})
	cx.Set(o, storage.NamedKey(cx.Key(KeyLastIndex)), storage.Number(0))
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
	return storage.NewRegExpObject(cx.RegExpShape(), cx.regExpLiteralState(site))
}

// regExpLiteralState returns the state that the RegExp objects of the literal site share.
func (cx *ExecutionContext) regExpLiteralState(site *regExpSite) *regExpState {
	if site.State == nil {
		site.State = &regExpState{
			Matcher: cx.compileRegExp(site.Literal),
			Source:  site.Literal.Pattern,
			Flags:   site.Literal.Flags,
			Legacy:  true,
			Realm:   cx.realmPart,
		}
	}
	return site.State
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
	f := regs[callee].O
	if dst == callee+2 {
		regs[dst] = Value{O: cx.regExpLiteralFor(f, site, cx.realm.regExpBorrowers)}
		return
	}
	if o := regs[dst].O; o != nil && o == site.Lent {
		// The register keeps the lent object, without a write barrier, when f borrows it.
		if lent := cx.regExpLiteralFor(f, site, cx.realm.regExpReceiverBorrowers); lent != o {
			regs[dst] = Value{O: lent}
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
	o := site.Lent
	if o == nil {
		lent := *cx.regExpLiteralState(site)
		lent.Lent = true
		o = storage.NewRegExpObject(cx.RegExpShape(), &lent)
		site.Lent = o
		return o
	}
	// A borrower that runs script code may still hold o, but no longer uses it.
	o.RenewRegExp(cx.RegExpShape())
	return o
}

// regExpLiteralMethod reads the method key of a RegExp object for the literal site, using the
// cache c, and returns it with the object to call it on. When reading it runs no code, that
// object is the lent object of the literal, which [ExecutionContext.lendRegExpLiteral] replaces before the call.
func (cx *ExecutionContext) regExpLiteralMethod(site *regExpSite, key *Object, c *propCache) (method Value, this *Object) {
	o := cx.resetLentRegExp(site)
	e := &c.CacheEntry
	if o.Shape != e.Shape {
		e = &c.Second
	}
	if o.Shape == e.Shape && e.Kind == cacheProto && e.Epoch == cx.ProtoEpoch {
		if validateExec {
			cx.checkEntryRead(nil, 0, Value{O: o}, o, key, e)
		}
		return *e.Ref, o
	}
	if v, ok := cx.GetVWithoutCall(Value{O: o}, key, c); ok {
		return v, o
	}
	o = cx.newRegExpLiteral(site)
	return cx.GetV(Value{O: o}, key), o
}

// OwnRegExp returns v, or a new RegExp object for the same literal when v is a RegExp object
// lent by [ExecutionContext.LendRegExpLiterals]. v must not have been used since it was lent.
func (cx *ExecutionContext) OwnRegExp(v Value) Value {
	if o := v.O; o != nil && o.Class == classRegExp {
		if st := o.ExtState().(*regExpState); st.Lent {
			own := *st
			own.Lent = false
			return Value{O: storage.NewRegExpObject(cx.RegExpShape(), &own)}
		}
	}
	return v
}

// IsRegExpOfCurrentRealm reports whether the [[Realm]] of the RegExp object o is the current realm.
func (cx *ExecutionContext) IsRegExpOfCurrentRealm(o *Object) bool {
	return o.ExtState().(*regExpState).Realm == cx.realmPart
}

// MatchRegExp matches re against the string s at the code unit index start, or after it unless re
// is sticky. It returns the code unit indices of the captures, or nil when there is no match. They
// are valid until the next match.
func (q QuietContext) MatchRegExp(re *regexp.Regexp, s Value, start int) []int {
	m := &q.realm.RegExpMatcher
	str := s.Str()
	m.SetASCII(str, s.IsASCII())
	return re.Match(m, str, start, q)
}

// FindRegExp is like [QuietContext.MatchRegExp], but it finds the first match at start or after it
// even when re is sticky.
func (q QuietContext) FindRegExp(re *regexp.Regexp, s Value, start int) []int {
	m := &q.realm.RegExpMatcher
	str := s.Str()
	m.SetASCII(str, s.IsASCII())
	return re.Find(m, str, start, q)
}

// RegExpMatchSpan returns the positions in the subject of the capture i of the last match, which
// must have succeeded.
func (q QuietContext) RegExpMatchSpan(i int) (start, end wtf8.Pos) {
	return q.realm.RegExpMatcher.Span(i)
}

// RegExpFlagsString returns the string of the flags f in the order of the flags getter.
func (cx *ExecutionContext) RegExpFlagsString(f regexp.Flags) Value {
	t := cx.realm.RegExpFlagStrings
	if t == nil {
		t = new([1 << 8]Value)
		cx.realm.RegExpFlagStrings = t
	}
	v := &t[f]
	if !v.IsString() {
		*v = cx.NewString(wtf8.String(f.String()))
	}
	return *v
}

// RecordRegExpMatch updates the legacy static properties of %RegExp% after a match of the RegExp
// object r against s with the capture indices caps. It does nothing when the [[Realm]] of r is not
// the current realm.
func (q QuietContext) RecordRegExpMatch(r *Object, s Value, caps []int) {
	st := r.ExtState().(*regExpState)
	if st.Realm != q.realm {
		return
	}
	if st.Legacy {
		q.UpdateLegacyRegExpStaticProperties(s, caps)
		return
	}
	// This is InvalidateLegacyRegExpStaticProperties.
	l := &q.realm.LegacyRegExp
	l.Input = storage.Undefined
	l.Str = storage.Undefined
	l.Caps = l.Caps[:0]
}

// UpdateLegacyRegExpStaticProperties implements UpdateLegacyRegExpStaticProperties for %RegExp%
// after a match against s with the capture indices caps.
func (q QuietContext) UpdateLegacyRegExpStaticProperties(s Value, caps []int) {
	l := &q.realm.LegacyRegExp
	l.Input = s
	l.Str = s
	l.Caps = append(l.Caps[:0], caps...)
}

// LegacyRegExpInput returns the [[RegExpInput]] slot of %RegExp%, or undefined for empty.
func (cx *ExecutionContext) LegacyRegExpInput() Value {
	return cx.realm.LegacyRegExp.Input
}

// SetLegacyRegExpInput sets the [[RegExpInput]] slot of %RegExp% to the string s.
func (cx *ExecutionContext) SetLegacyRegExpInput(s Value) {
	cx.realm.LegacyRegExp.Input = s
}

// LegacyRegExpMatch returns the subject and the capture indices of the last match that the legacy
// static properties of %RegExp% show. s is undefined when they are unavailable. Before the first
// match, s is the empty string and caps is empty. caps is valid until the next match.
func (cx *ExecutionContext) LegacyRegExpMatch() (s Value, caps []int) {
	l := &cx.realm.LegacyRegExp
	return l.Str, l.Caps
}

// RegExpLastIndex returns the value of the own lastIndex property of o, or false when o may not
// have it as a writable data property where [ExecutionContext.RegExpAlloc] puts it.
func (q QuietContext) RegExpLastIndex(o *Object) (Value, bool) {
	return q.realm.RegExpLastIndex(o)
}

// SetRegExpLastIndex sets the own lastIndex property of o to v and reports true, or reports
// false when o may not have it as a writable data property where [ExecutionContext.RegExpAlloc] puts it.
func (q QuietContext) SetRegExpLastIndex(o *Object, v Value) bool {
	return q.realm.SetRegExpLastIndex(o, v)
}

// UnmodifiedRegExpMatcher returns the compiled pattern of o when o is an initialized RegExp object
// whose prototype is %RegExp.prototype% and whose only own property is a writable lastIndex, while
// the properties recorded by [ExecutionContext.RecordRegExpOriginals] are unchanged, and nil
// otherwise. When it is not nil, those properties are the only ones that reading the recorded keys
// from o finds, and lastIndex is an ordinary data property.
func (q QuietContext) UnmodifiedRegExpMatcher(o *Object) *regexp.Regexp {
	return q.realm.UnmodifiedRegExpMatcher(o)
}
