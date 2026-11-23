// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// regexpEnv is the state that the RegExp built-ins of one realm share.
type regexpEnv struct {
	m regexp.Matcher

	// exec is the original RegExp.prototype.exec. When a RegExp object's exec is this function,
	// the algorithms that call it use the matcher directly, without creating match arrays.
	exec *engine.Object

	lastIndex, index, groups, flags, source, execKey *engine.Object

	// flagStrings holds the value of the flags getter for each set of the 8 flags, once computed.
	flagStrings *[1 << 8]engine.Value

	legacy legacyStatics
}

// legacyStatics holds the values of the legacy static properties of RegExp.
type legacyStatics struct {
	// input and str are undefined when their respective statics are unavailable.
	input engine.Value
	// str and caps are the subject and the capture indices of the last match, if any.
	str  engine.Value
	caps []int
}

// record updates the legacy static properties after a match of r.
func (l *legacyStatics) record(r *engine.Object, s engine.Value, caps []int) {
	l.recordLegacy(r.RegExpLegacyFeaturesEnabled(), s, caps)
}

// recordLegacy updates the legacy static properties after a match of a RegExp object
// whose [[LegacyFeaturesEnabled]] is legacy.
func (l *legacyStatics) recordLegacy(legacy bool, s engine.Value, caps []int) {
	if !legacy {
		l.input = engine.Undefined
		l.str = engine.Undefined
		l.caps = l.caps[:0]
		return
	}
	l.input = s
	l.str = s
	l.caps = append(l.caps[:0], caps...)
}

func (e *regexpEnv) key(k *engine.Object) engine.PropKey {
	return engine.NamedKey(k)
}

// flagsString returns the string of the flags f in the order of the flags getter.
func (e *regexpEnv) flagsString(cx *engine.ExecutionContext, f regexp.Flags) engine.Value {
	if e.flagStrings == nil {
		e.flagStrings = new([1 << 8]engine.Value)
	}
	v := &e.flagStrings[f]
	if !v.IsString() {
		*v = cx.NewString(wtf8.String(f.String()))
	}
	return *v
}

// getFlags returns ToString(Get(r, "flags")).
func (e *regexpEnv) getFlags(cx *engine.ExecutionContext, r *engine.Object) engine.Value {
	if cx.IsUnmodifiedRegExp(r) {
		return e.flagsString(cx, r.RegExpMatcher().Flags())
	}
	return cx.ToStringValue(cx.Get(r, e.key(e.flags)))
}

// getLastIndex returns ToLength(Get(r, "lastIndex")).
func (e *regexpEnv) getLastIndex(cx *engine.ExecutionContext, r *engine.Object) int64 {
	v, ok := cx.RegExpLastIndex(r)
	if !ok {
		v = cx.Get(r, e.key(e.lastIndex))
	}
	if v.IsNumber() {
		return numericLastIndex(v.Number())
	}
	return numconv.ToLength(cx.ToNumber(v))
}

func numericLastIndex(n float64) int64 {
	if n >= 0 && n <= numconv.MaxSafeInteger {
		return int64(n)
	}
	return numconv.ToLength(n)
}

func (e *regexpEnv) setLastIndex(cx *engine.ExecutionContext, r *engine.Object, n int64) {
	if !cx.SetRegExpLastIndex(r, engine.Number(float64(n))) {
		cx.Set(r, e.key(e.lastIndex), engine.Number(float64(n)))
	}
}

// advanceStringIndex implements AdvanceStringIndex.
func advanceStringIndex(s engine.Value, index int64, unicode bool) int64 {
	if !unicode {
		return index + 1
	}
	n := int64(s.StringLength())
	if index+1 >= n {
		return index + 1
	}
	if u := s.UnitAt(int(index)); wtf8.IsHighSurrogate(rune(u)) {
		if lo := s.UnitAt(int(index) + 1); wtf8.IsLowSurrogate(rune(lo)) {
			return index + 2
		}
	}
	return index + 1
}

// subject returns the text of the string s for a match, and tells the matcher whether it is
// ASCII, which s remembers.
func (e *regexpEnv) subject(s engine.Value) wtf8.String {
	str := s.Str()
	e.m.SetASCII(str, s.IsASCII())
	return str
}

// builtinExecRaw implements RegExpBuiltinExec up to the creation of the match array.
// It returns the code unit indices of the captures, which are valid until the next match,
// or nil when there is no match.
func (e *regexpEnv) builtinExecRaw(cx *engine.ExecutionContext, r *engine.Object, s engine.Value, lastIndex int64) []int {
	re := r.RegExpMatcher()
	f := re.Flags()
	global, sticky := f&regexp.FlagGlobal != 0, f&regexp.FlagSticky != 0
	if !global && !sticky {
		lastIndex = 0
	}
	// No string is shorter than a lastIndex of 0, which matches that are neither global nor sticky
	// start at.
	if lastIndex > 0 && lastIndex > int64(s.StringLength()) {
		if global || sticky {
			e.setLastIndex(cx, r, 0)
		}
		return nil
	}
	caps := re.Match(&e.m, e.subject(s), int(lastIndex), cx)
	if caps == nil {
		if global || sticky {
			e.setLastIndex(cx, r, 0)
		}
		return nil
	}
	if global || sticky {
		e.setLastIndex(cx, r, int64(caps[1]))
	}
	if cx.IsRegExpOfCurrentRealm(r) {
		e.legacy.record(r, s, caps)
	}
	return caps
}

// capture returns the capture i of caps in s, or undefined.
func capture(cx *engine.ExecutionContext, s engine.Value, caps []int, i int) engine.Value {
	a, b := caps[2*i], caps[2*i+1]
	if a < 0 {
		return engine.Undefined
	}
	if a == 0 && b == s.StringLength() {
		return s
	}
	return cx.Substring(s, a, b)
}

// groupsObject returns the groups object of a match of re, or undefined when re has no named groups.
func groupsObject(cx *engine.ExecutionContext, re *regexp.Regexp, vals func(i int) engine.Value) engine.Value {
	names := re.Names()
	if len(names) == 0 {
		return engine.Undefined
	}
	g := cx.NewObject(nil)
	matched := map[wtf8.String]bool{}
	for i := 1; i <= re.GroupCount(); i++ {
		name := re.GroupName(i)
		if name == "" || matched[name] {
			continue
		}
		v := vals(i)
		if !v.IsUndefined() {
			matched[name] = true
		}
		cx.CreateDataPropertyOrThrow(g, engine.NamedKey(cx.Intern(name)), v)
	}
	return engine.ObjectValue(g)
}

// builtinExec implements RegExpBuiltinExec.
func (e *regexpEnv) builtinExec(cx *engine.ExecutionContext, r *engine.Object, s engine.Value) engine.Value {
	// Converting lastIndex can run JavaScript that recompiles r.
	lastIndex := e.getLastIndex(cx, r)
	caps := e.builtinExecRaw(cx, r, s, lastIndex)
	if caps == nil {
		return engine.Null
	}
	re := r.RegExpMatcher()
	n := re.GroupCount()
	nprops := 3
	if re.Flags()&regexp.FlagHasIndices != 0 {
		nprops = 4
	}
	a := cx.NewArrayWithRoom(n+1, nprops)
	// The matcher still holds the match, whose positions in a string that is not ASCII spare a
	// search for the code units of the captures.
	ascii := s.IsASCII()
	for i := 0; i <= n; i++ {
		if ascii || caps[2*i] < 0 {
			a.SetElement(int64(i), capture(cx, s, caps, i))
			continue
		}
		start, end := e.m.Span(i)
		a.SetElement(int64(i), cx.SubstringBetween(s, start, end))
	}
	var pairs []engine.Value
	if re.Flags()&regexp.FlagHasIndices != 0 {
		pairs = make([]engine.Value, n+1)
		for i := range pairs {
			if caps[2*i] >= 0 {
				pairs[i] = engine.ObjectValue(cx.NewArrayPair(
					engine.Number(float64(caps[2*i])),
					engine.Number(float64(caps[2*i+1])),
				))
			}
		}
	}
	index := engine.Number(float64(caps[0]))
	groups := groupsObject(cx, re, func(i int) engine.Value {
		v, _ := a.Element(int64(i))
		return v
	})
	if pairs == nil {
		cx.DefineMatchResultProperties(a, index, s, groups)
		return engine.ObjectValue(a)
	}
	ia := cx.NewArray(pairs)
	cx.CreateDataPropertyOrThrow(ia, e.key(e.groups), groupsObject(cx, re, func(i int) engine.Value { return pairs[i] }))
	cx.DefineMatchResultPropertiesWithIndices(a, index, s, groups, engine.ObjectValue(ia))
	return engine.ObjectValue(a)
}

// execMatch performs RegExpExec, returning capture indices or the result object, or both nil for no match.
func (e *regexpEnv) execMatch(cx *engine.ExecutionContext, r *engine.Object, s engine.Value) ([]int, *engine.Object) {
	var exec, index engine.Value
	if cx.IsUnmodifiedRegExp(r) {
		index, _ = cx.RegExpLastIndex(r)
		exec = engine.ObjectValue(e.exec)
	} else {
		exec = cx.Get(r, e.key(e.execKey))
		if exec.IsObject() && exec.Object() == e.exec && r.RegExpMatcher() != nil {
			if d, _ := cx.GetOwnProperty(r, e.key(e.lastIndex)); d.Writable() {
				index = d.Value()
			}
		}
	}
	// With a writable numeric lastIndex, the original exec can run without a native frame.
	// Coercion can otherwise run JavaScript, and a read-only lastIndex can throw on a write.
	if index.IsNumber() {
		return e.builtinExecRaw(cx, r, s, numericLastIndex(index.Number())), nil
	}
	result := e.regExpExecWith(cx, r, s, exec)
	if result.IsNull() {
		return nil, nil
	}
	return nil, result.Object()
}

// cloneRegExp returns a new RegExp object with the pattern src and the flags flags. It does what
// Construct(%RegExp%, « r, flags ») does for a RegExp object r with the source src for which
// [engine.ExecutionContext.IsUnmodifiedRegExp] holds.
func cloneRegExp(cx *engine.ExecutionContext, src wtf8.String, flags engine.Value) *engine.Object {
	return cx.RegExpInitialize(cx.RegExpAlloc(cx.Intrinsic(engine.IntrinsicRegExp)), cx.NewString(src), flags)
}

// regExpExecWith implements RegExpExec with the value of r.exec already read.
func (e *regexpEnv) regExpExecWith(cx *engine.ExecutionContext, r *engine.Object, s engine.Value, exec engine.Value) engine.Value {
	if exec.IsCallable() {
		res := cx.Call(exec.Object(), engine.ObjectValue(r), s)
		if !res.IsObject() && !res.IsNull() {
			cx.ThrowTypeError("the result of a RegExp exec method must be an object or null")
		}
		return res
	}
	if r.RegExpMatcher() == nil {
		cx.ThrowTypeError("RegExp exec method called on an incompatible receiver")
	}
	return e.builtinExec(cx, r, s)
}

// regExpExec implements RegExpExec.
func (e *regexpEnv) regExpExec(cx *engine.ExecutionContext, r *engine.Object, s engine.Value) engine.Value {
	exec := cx.Get(r, e.key(e.execKey))
	return e.regExpExecWith(cx, r, s, exec)
}

// isRegExp implements IsRegExp.
func isRegExp(cx *engine.ExecutionContext, v engine.Value) bool {
	if !v.IsObject() {
		return false
	}
	if m := cx.GetV(v, cx.WellKnownSymbol(engine.SymbolMatch)); !m.IsUndefined() {
		return engine.ToBoolean(m)
	}
	return v.Object().IsRegExpObject()
}

// requireGlobalRegExp throws the TypeError of String.prototype[name] when o is a RegExp
// object, by IsRegExp, whose flags do not include g.
func requireGlobalRegExp(cx *engine.ExecutionContext, o *engine.Object, name string) {
	var global bool
	if cx.IsUnmodifiedRegExp(o) {
		global = o.RegExpMatcher().Flags()&regexp.FlagGlobal != 0
	} else {
		if !isRegExp(cx, engine.ObjectValue(o)) {
			return
		}
		flags := cx.GetV(engine.ObjectValue(o), cx.Intern("flags"))
		cx.RequireObjectCoercible(flags, "String.prototype."+name)
		global = strings.Contains(string(cx.ToString(flags)), "g")
	}
	if !global {
		cx.ThrowTypeError(name + " must be called with a global RegExp")
	}
}

// regExpCreate implements RegExpCreate.
func regExpCreate(cx *engine.ExecutionContext, p, f engine.Value) *engine.Object {
	return cx.RegExpInitialize(cx.RegExpAlloc(cx.Intrinsic(engine.IntrinsicRegExp)), p, f)
}

// speciesConstructor implements SpeciesConstructor.
func speciesConstructor(cx *engine.ExecutionContext, o *engine.Object, def *engine.Object) *engine.Object {
	c := cx.Get(o, engine.NamedKey(cx.Key(engine.KeyConstructor)))
	if c.IsUndefined() {
		return def
	}
	if !c.IsObject() {
		cx.ThrowTypeError("object.constructor is not an object")
	}
	s := cx.GetV(c, cx.WellKnownSymbol(engine.SymbolSpecies))
	if s.IsNullish() {
		return def
	}
	if s.IsObject() && cx.IsConstructor(s.Object()) {
		return s.Object()
	}
	cx.ThrowTypeError("object.constructor[Symbol.species] is not a constructor")
	return nil
}

// escapeRegExpPattern implements EscapeRegExpPattern.
func escapeRegExpPattern(src wtf8.String) wtf8.String {
	if src == "" {
		return "(?:)"
	}
	if !strings.ContainsAny(string(src), "/\n\r  ") {
		return src
	}
	var b strings.Builder
	inClass := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '\\':
			b.WriteByte(c)
			if i+1 < len(src) {
				i++
				switch rest := src[i:]; {
				case rest[0] == '\n':
					b.WriteString("n")
				case rest[0] == '\r':
					b.WriteString("r")
				case strings.HasPrefix(string(rest), "\u2028"):
					b.WriteString("u2028")
					i += 2
				case strings.HasPrefix(string(rest), "\u2029"):
					b.WriteString("u2029")
					i += 2
				default:
					b.WriteByte(src[i])
				}
			}
			continue
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				b.WriteString("\\/")
				continue
			}
		case '\n':
			b.WriteString("\\n")
			continue
		case '\r':
			b.WriteString("\\r")
			continue
		case 0xE2:
			if i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xA8 || src[i+2] == 0xA9) {
				if src[i+2] == 0xA8 {
					b.WriteString("\\u2028")
				} else {
					b.WriteString("\\u2029")
				}
				i += 2
				continue
			}
		}
		b.WriteByte(c)
	}
	return wtf8.String(b.String())
}

// flagProps lists the flag accessors of RegExp.prototype in the order of the flags getter.
var flagProps = []struct {
	name string
	flag regexp.Flags
}{
	{
		name: "hasIndices",
		flag: regexp.FlagHasIndices,
	},
	{
		name: "global",
		flag: regexp.FlagGlobal,
	},
	{
		name: "ignoreCase",
		flag: regexp.FlagIgnoreCase,
	},
	{
		name: "multiline",
		flag: regexp.FlagMultiline,
	},
	{
		name: "dotAll",
		flag: regexp.FlagDotAll,
	},
	{
		name: "unicode",
		flag: regexp.FlagUnicode,
	},
	{
		name: "unicodeSets",
		flag: regexp.FlagUnicodeSets,
	},
	{
		name: "sticky",
		flag: regexp.FlagSticky,
	},
}

func initRegExp(cx *engine.ExecutionContext) {
	env := &regexpEnv{
		lastIndex: cx.Intern("lastIndex"),
		index:     cx.Intern("index"),
		groups:    cx.Intern("groups"),
		flags:     cx.Intern("flags"),
		source:    cx.Intern("source"),
		execKey:   cx.Intern("exec"),
	}
	proto := cx.Intrinsic(engine.IntrinsicRegExpPrototype)
	ctor := constructor(cx, "RegExp", 2, engine.IntrinsicRegExpPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		pattern, flags := arg(args, 0), arg(args, 1)
		patternIsRegExp := isRegExp(cx, pattern)
		nt := cx.GetNewTarget()
		if nt == nil {
			nt = cx.Intrinsic(engine.IntrinsicRegExp)
			if patternIsRegExp && flags.IsUndefined() {
				pc := cx.GetV(pattern, cx.Key(engine.KeyConstructor))
				if pc.IsObject() && pc.Object() == nt {
					return pattern
				}
			}
		}
		var p, f engine.Value
		switch {
		case pattern.IsObject() && pattern.Object().IsRegExpObject():
			src, fl := pattern.Object().RegExpOriginal()
			p = cx.NewString(src)
			if flags.IsUndefined() {
				f = cx.NewString(fl)
			} else {
				f = flags
			}
		case patternIsRegExp:
			p = cx.GetV(pattern, env.source)
			if flags.IsUndefined() {
				f = cx.GetV(pattern, env.flags)
			} else {
				f = flags
			}
		default:
			p, f = pattern, flags
		}
		o := cx.RegExpAlloc(nt)
		return engine.ObjectValue(cx.RegExpInitialize(o, p, f))
	})
	cx.SetIntrinsic(engine.IntrinsicRegExp, ctor)
	cx.DefineAccessor(ctor, cx.WellKnownSymbol(engine.SymbolSpecies), cx.NewNative("get [Symbol.species]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return this
	}), nil, engine.FlagConfigurable)

	thisRegExp := func(cx *engine.ExecutionContext, this engine.Value, what string) *engine.Object {
		if !this.IsObject() || this.Object().RegExpMatcher() == nil {
			cx.ThrowTypeError("RegExp.prototype." + what + " requires that 'this' be a RegExp object")
		}
		return this.Object()
	}

	// exec and test borrow the RegExp object of a literal they are called on. They run no script
	// code before the last use of it, except the conversion of an object argument.
	env.exec = method(cx, proto, "exec", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if arg(args, 0).IsObject() {
			this = cx.OwnRegExp(this)
		}
		r := thisRegExp(cx, this, "exec")
		s := cx.ToStringValue(arg(args, 0))
		return env.builtinExec(cx, r, s)
	})
	cx.LendRegExpLiteralReceivers(env.exec)
	cx.LendRegExpLiteralReceivers(method(cx, proto, "test", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if arg(args, 0).IsObject() {
			this = cx.OwnRegExp(this)
		}
		r := thisObject(cx, this, "RegExp.prototype.test")
		s := cx.ToStringValue(arg(args, 0))
		caps, result := env.execMatch(cx, r, s)
		return engine.Bool(caps != nil || result != nil)
	}))
	method(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		r := thisObject(cx, this, "RegExp.prototype.toString")
		src := cx.ToString(cx.Get(r, env.key(env.source)))
		fl := cx.ToString(cx.Get(r, env.key(env.flags)))
		checkStringLength(cx, len(src)+len(fl)+2)
		return cx.NewString(wtf8.Concat(wtf8.Concat("/"+src, "/"), fl))
	})
	method(cx, proto, "compile", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := thisRegExp(cx, this, "compile")
		if !cx.IsRegExpOfCurrentRealm(o) {
			cx.ThrowTypeError("RegExp.prototype.compile cannot be used on a RegExp object of another realm")
		}
		if !o.RegExpLegacyFeaturesEnabled() {
			cx.ThrowTypeError("RegExp.prototype.compile cannot be used on a subclass instance")
		}
		pattern, flags := arg(args, 0), arg(args, 1)
		p, f := pattern, flags
		if pattern.IsObject() && pattern.Object().IsRegExpObject() {
			if !flags.IsUndefined() {
				cx.ThrowTypeError("cannot supply flags when constructing one RegExp from another")
			}
			src, fl := pattern.Object().RegExpOriginal()
			p, f = cx.NewString(src), cx.NewString(fl)
		}
		return engine.ObjectValue(cx.RegExpInitialize(o, p, f))
	})
	for _, fp := range flagProps {
		getter(cx, proto, cx.Intern(wtf8.String(fp.name)), wtf8.String(fp.name), func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			if !this.IsObject() {
				cx.ThrowTypeError("RegExp.prototype." + fp.name + " getter called on non-object")
			}
			re := this.Object().RegExpMatcher()
			if re == nil {
				if this.Object() == cx.Intrinsic(engine.IntrinsicRegExpPrototype) {
					return engine.Undefined
				}
				cx.ThrowTypeError("RegExp.prototype." + fp.name + " getter called on non-RegExp object")
			}
			return engine.Bool(re.Flags()&fp.flag != 0)
		})
	}
	flagKeys := make([]engine.PropKey, len(flagProps))
	for i, fp := range flagProps {
		flagKeys[i] = engine.NamedKey(cx.Intern(wtf8.String(fp.name)))
	}
	getter(cx, proto, env.flags, "flags", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		r := thisObject(cx, this, "RegExp.prototype.flags getter")
		if cx.IsUnmodifiedRegExp(r) {
			return env.flagsString(cx, r.RegExpMatcher().Flags())
		}
		var f regexp.Flags
		for i, fp := range flagProps {
			if engine.ToBoolean(cx.Get(r, flagKeys[i])) {
				f |= fp.flag
			}
		}
		return env.flagsString(cx, f)
	})
	getter(cx, proto, env.source, "source", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			cx.ThrowTypeError("RegExp.prototype.source getter called on non-object")
		}
		o := this.Object()
		if o.RegExpMatcher() == nil {
			if o == cx.Intrinsic(engine.IntrinsicRegExpPrototype) {
				return cx.NewString("(?:)")
			}
			cx.ThrowTypeError("RegExp.prototype.source getter called on non-RegExp object")
		}
		src, _ := o.RegExpOriginal()
		escaped := escapeRegExpPattern(src)
		checkStringLength(cx, len(escaped))
		return cx.NewString(escaped)
	})

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolMatch), "[Symbol.match]", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(cx, this, "RegExp.prototype[Symbol.match]")
		s := cx.ToStringValue(arg(args, 0))
		flags := env.getFlags(cx, rx).Str()
		if !strings.Contains(string(flags), "g") {
			return env.regExpExec(cx, rx, s)
		}
		fullUnicode := strings.ContainsAny(string(flags), "uv")
		env.setLastIndex(cx, rx, 0)
		var out []engine.Value
		for {
			var matchStr engine.Value
			caps, result := env.execMatch(cx, rx, s)
			if caps != nil {
				matchStr = capture(cx, s, caps, 0)
			} else if result != nil {
				matchStr = cx.ToStringValue(cx.Get(result, engine.IndexKey(0)))
			} else {
				break
			}
			out = append(out, matchStr)
			if matchStr.Str() == "" {
				i := env.getLastIndex(cx, rx)
				env.setLastIndex(cx, rx, advanceStringIndex(s, i, fullUnicode))
			}
		}
		if len(out) == 0 {
			return engine.Null
		}
		return engine.ObjectValue(cx.NewArray(out))
	})

	iterProto := cx.NewObject(cx.Intrinsic(engine.IntrinsicIteratorPrototype))
	iteratorNext(cx, iterProto, engine.IteratorMethodRealm, func(cx *engine.ExecutionContext, this engine.Value) (engine.Value, bool) {
		st, ok := engine.IteratorState(this).(*regexpStringIterator)
		if !ok {
			cx.ThrowTypeError("%RegExpStringIteratorPrototype%.next requires that 'this' be a RegExp String Iterator")
		}
		if st.done {
			return engine.Undefined, false
		}
		m := env.regExpExec(cx, st.r, st.s)
		if m.IsNull() {
			st.done = true
			return engine.Undefined, false
		}
		if !st.global {
			st.done = true
			return m, true
		}
		if cx.ToString(cx.Get(m.Object(), engine.IndexKey(0))) == "" {
			i := env.getLastIndex(cx, st.r)
			env.setLastIndex(cx, st.r, advanceStringIndex(st.s, i, st.fullUnicode))
		}
		return m, true
	})
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("RegExp String Iterator"), engine.FlagConfigurable)

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolMatchAll), "[Symbol.matchAll]", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		r := thisObject(cx, this, "RegExp.prototype[Symbol.matchAll]")
		s := cx.ToStringValue(arg(args, 0))
		unmodified := cx.IsUnmodifiedRegExp(r)
		var c *engine.Object
		if !unmodified {
			c = speciesConstructor(cx, r, cx.Intrinsic(engine.IntrinsicRegExp))
		}
		flags := env.getFlags(cx, r)
		var matcher *engine.Object
		if unmodified {
			src, _ := r.RegExpOriginal()
			matcher = cloneRegExp(cx, src, flags)
		} else {
			matcher = cx.Construct(c, []engine.Value{engine.ObjectValue(r), flags}, c)
		}
		lastIndex := env.getLastIndex(cx, r)
		env.setLastIndex(cx, matcher, lastIndex)
		fl := flags.Str()
		return engine.ObjectValue(cx.NewIteratorObject(iterProto, &regexpStringIterator{
			r:           matcher,
			s:           s,
			global:      strings.Contains(string(fl), "g"),
			fullUnicode: strings.ContainsAny(string(fl), "uv"),
		}))
	})

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolReplace), "[Symbol.replace]", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(cx, this, "RegExp.prototype[Symbol.replace]")
		s := cx.ToStringValue(arg(args, 0))
		return env.replace(cx, rx, s, arg(args, 1))
	})

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolSearch), "[Symbol.search]", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(cx, this, "RegExp.prototype[Symbol.search]")
		s := cx.ToStringValue(arg(args, 0))
		key := env.key(env.lastIndex)
		prev := cx.Get(rx, key)
		if !engine.SameValue(prev, engine.Number(0)) {
			cx.Set(rx, key, engine.Number(0))
		}
		index := engine.Number(-1)
		caps, result := env.execMatch(cx, rx, s)
		if caps != nil {
			index = engine.Number(float64(caps[0]))
		}
		if cur := cx.Get(rx, key); !engine.SameValue(cur, prev) {
			cx.Set(rx, key, prev)
		}
		if result != nil {
			index = cx.Get(result, env.key(env.index))
		}
		return index
	})

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolSplit), "[Symbol.split]", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(cx, this, "RegExp.prototype[Symbol.split]")
		s := cx.ToStringValue(arg(args, 0))
		return env.split(cx, rx, s, arg(args, 1))
	})

	method(cx, ctor, "escape", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		if !v.IsString() {
			cx.ThrowTypeError("RegExp.escape requires a string")
		}
		return cx.NewString(regExpEscape(cx, v.Str()))
	})

	initLegacyStatics(cx, ctor, env)

	// The fast paths read these properties of an unmodified RegExp object without a lookup:
	// the flag accessors, exec, constructor and @@species for SpeciesConstructor, and @@match
	// for the IsRegExp of the RegExp constructor. The String methods that borrow RegExp objects
	// read @@match, @@replace, @@search and @@split without running script code.
	protoKeys := []*engine.Object{
		env.flags,
		env.execKey,
		cx.Key(engine.KeyConstructor),
		cx.WellKnownSymbol(engine.SymbolMatch),
		cx.WellKnownSymbol(engine.SymbolReplace),
		cx.WellKnownSymbol(engine.SymbolSearch),
		cx.WellKnownSymbol(engine.SymbolSplit),
	}
	for _, fp := range flagProps {
		protoKeys = append(protoKeys, cx.Intern(wtf8.String(fp.name)))
	}
	cx.RecordRegExpOriginals(protoKeys, []*engine.Object{cx.WellKnownSymbol(engine.SymbolSpecies)})
}

type regexpStringIterator struct {
	r           *engine.Object
	s           engine.Value
	global      bool
	fullUnicode bool
	done        bool
}

// match is one match that RegExp.prototype[Symbol.replace] uses.
type match struct {
	// res is the match array, when the original exec did not produce the match.
	res *engine.Object
	// matcher produced the capture indices. A later exec lookup can recompile the receiver.
	matcher *regexp.Regexp
	// capsStart and capsEnd are the range of the capture indices of a match of the original
	// exec in the buffer that holds them.
	capsStart, capsEnd int
}

func (e *regexpEnv) replace(cx *engine.ExecutionContext, rx *engine.Object, s engine.Value, replaceValue engine.Value) engine.Value {
	functional := replaceValue.IsCallable()
	var template wtf8.String
	if !functional {
		template = cx.ToString(replaceValue)
	}
	flags := e.getFlags(cx, rx).Str()
	global := strings.Contains(string(flags), "g")
	fullUnicode := false
	if global {
		fullUnicode = strings.ContainsAny(string(flags), "uv")
		e.setLastIndex(cx, rx, 0)
	}
	length := s.StringLength()
	var b wtf8.Builder
	next := 0
	found := false
	// results holds the matches whose replacements are computed after the last match.
	// A match of the original exec with a string template is replaced as soon as it is
	// found, as long as no match is pending, since GetSubstitution then runs no JavaScript.
	var resultsArr [4]match
	var capsArr [16]int
	results, capsBuf := resultsArr[:0], capsArr[:0]
	for {
		caps, res := e.execMatch(cx, rx, s)
		if caps == nil && res == nil {
			break
		}
		found = true
		var empty bool
		if caps != nil {
			empty = caps[0] == caps[1]
			re := rx.RegExpMatcher()
			if !functional && len(results) == 0 {
				if b.Len() == 0 {
					b.Grow(len(s.Str()))
				}
				next = substituteMatch(cx, &b, re, s, caps, template, next)
			} else {
				start := len(capsBuf)
				capsBuf = append(capsBuf, caps...)
				results = append(results, match{
					matcher:   re,
					capsStart: start,
					capsEnd:   len(capsBuf),
				})
			}
		} else {
			results = append(results, match{res: res})
			if global {
				empty = cx.ToString(cx.Get(res, engine.IndexKey(0))) == ""
			}
		}
		if !global {
			break
		}
		if empty {
			i := e.getLastIndex(cx, rx)
			e.setLastIndex(cx, rx, advanceStringIndex(s, i, fullUnicode))
		}
	}
	if !found {
		return s
	}
	for _, r := range results {
		var matched wtf8.String
		var position int
		var captures []engine.Value
		var named engine.Value
		if re := r.matcher; re != nil {
			caps := capsBuf[r.capsStart:r.capsEnd]
			position = caps[0]
			matched = s.Substring(caps[0], caps[1])
			n := len(caps)/2 - 1
			captures = make([]engine.Value, n)
			for i := range captures {
				captures[i] = capture(cx, s, caps, i+1)
			}
			named = engine.Undefined
			if len(re.Names()) > 0 && (functional || strings.Contains(string(template), "$<")) {
				named = groupsObject(cx, re, func(i int) engine.Value { return captures[i-1] })
			}
		} else {
			res := r.res
			n := max(cx.LengthOfArrayLike(res)-1, 0)
			matched = cx.ToString(cx.Get(res, engine.IndexKey(0)))
			p := cx.ToIntegerOrInfinity(cx.Get(res, e.key(e.index)))
			position = int(math.Max(math.Min(p, float64(length)), 0))
			captures = make([]engine.Value, n)
			for i := range captures {
				c := getAt(cx, res, int64(i)+1)
				if !c.IsUndefined() {
					c = cx.ToStringValue(c)
				}
				captures[i] = c
			}
			named = cx.Get(res, e.key(e.groups))
		}
		if !functional {
			if !named.IsUndefined() {
				named = engine.ObjectValue(cx.ToObject(named))
			}
			if position < next {
				// The replacement is unused, but computing it can run JavaScript.
				var rb wtf8.Builder
				writeSubstitution(cx, &rb, matched, s, position, substCaptures{vals: captures}, named, template)
				continue
			}
			writeLimited(cx, &b, s.Substring(next, position))
			writeSubstitution(cx, &b, matched, s, position, substCaptures{vals: captures}, named, template)
			next = position + wtf8.Len(matched)
			continue
		}
		callArgs := make([]engine.Value, 0, len(captures)+4)
		callArgs = append(callArgs, cx.NewString(matched))
		callArgs = append(callArgs, captures...)
		callArgs = append(callArgs, engine.Number(float64(position)), s)
		if !named.IsUndefined() {
			callArgs = append(callArgs, named)
		}
		replacement := cx.ToString(cx.Call(replaceValue.Object(), engine.Undefined, callArgs...))
		if position >= next {
			writeLimited(cx, &b, s.Substring(next, position))
			writeLimited(cx, &b, replacement)
			next = position + wtf8.Len(matched)
		}
	}
	if next >= length {
		return cx.NewString(b.String())
	}
	writeLimited(cx, &b, s.Substring(next, length))
	return cx.NewString(b.String())
}

// substituteMatch writes to b the part of s from next up to a match of re with the capture
// indices caps, followed by its replacement by template. It returns the index after the match.
func substituteMatch(cx *engine.ExecutionContext, b *wtf8.Builder, re *regexp.Regexp, s engine.Value, caps []int, template wtf8.String, next int) int {
	position := caps[0]
	if position < next {
		return next
	}
	writeLimited(cx, b, s.Substring(next, position))
	matched := s.Substring(caps[0], caps[1])
	if !strings.Contains(string(template), "$") {
		writeLimited(cx, b, template)
		return caps[1]
	}
	named := engine.Undefined
	if len(re.Names()) > 0 && strings.Contains(string(template), "$<") {
		named = groupsObject(cx, re, func(i int) engine.Value { return capture(cx, s, caps, i) })
	}
	writeSubstitution(cx, b, matched, s, position, substCaptures{str: s, caps: caps}, named, template)
	return caps[1]
}

func (e *regexpEnv) split(cx *engine.ExecutionContext, rx *engine.Object, s engine.Value, limit engine.Value) engine.Value {
	// For an unmodified rx, finding the constructor and constructing the splitter run no
	// JavaScript, so the splitter is only made when the algorithm can observe it.
	unmodified := cx.IsUnmodifiedRegExp(rx)
	var c *engine.Object
	if unmodified {
		c = cx.Intrinsic(engine.IntrinsicRegExp)
	} else {
		c = speciesConstructor(cx, rx, cx.Intrinsic(engine.IntrinsicRegExp))
	}
	flags := e.getFlags(cx, rx).Str()
	unicodeMatching := strings.ContainsAny(string(flags), "uv")
	newFlags := flags
	if !strings.Contains(string(flags), "y") {
		newFlags += "y"
	}
	var splitter *engine.Object
	var src wtf8.String
	var re *regexp.Regexp
	if unmodified {
		src, _ = rx.RegExpOriginal()
		re = rx.RegExpMatcher()
	} else {
		splitter = cx.Construct(c, []engine.Value{engine.ObjectValue(rx), cx.NewString(newFlags)}, c)
	}
	lim := uint32(math.MaxUint32)
	if !limit.IsUndefined() {
		lim = cx.ToUint32(limit)
	}
	var out []engine.Value
	if lim == 0 {
		return engine.ObjectValue(cx.NewArray(nil))
	}
	// When the splitter is a RegExp object that no code can see, and its exec is the original
	// one, searching for the next match is the same as trying every position. The matcher of rx
	// finds the same matches as that of the splitter, which differs only in the sticky flag.
	var fast bool
	if splitter == nil {
		// ToUint32 may have changed %RegExp.prototype%.
		fast = cx.IsUnmodifiedRegExp(rx)
		if !fast {
			splitter = cloneRegExp(cx, src, cx.NewString(newFlags))
		}
	}
	// Reading exec from a splitter that is not unmodified can run JavaScript, so it is read
	// only where RegExpExec reads it.
	if splitter != nil && c == cx.Intrinsic(engine.IntrinsicRegExp) && cx.IsUnmodifiedRegExp(splitter) {
		fast = true
		re = splitter.RegExpMatcher()
	}
	size := s.StringLength()
	if size == 0 {
		if fast {
			// The splitter is sticky and its lastIndex is 0, so it can only match at 0.
			if caps := re.Find(&e.m, e.subject(s), 0, cx); caps != nil {
				e.legacy.recordLegacy(true, s, caps)
				return engine.ObjectValue(cx.NewArray(nil))
			}
		} else {
			caps, result := e.execMatch(cx, splitter, s)
			if caps != nil || result != nil {
				return engine.ObjectValue(cx.NewArray(nil))
			}
		}
		return engine.ObjectValue(cx.NewArray([]engine.Value{s}))
	}
	p, q := 0, 0
	for q < size {
		var caps []int
		var z engine.Value
		var end int
		if fast {
			caps = re.Find(&e.m, e.subject(s), q, cx)
			if caps == nil || caps[0] >= size {
				break
			}
			e.legacy.recordLegacy(true, s, caps)
			q = caps[0]
			end = caps[1]
		} else {
			e.setLastIndex(cx, splitter, int64(q))
			z = e.regExpExec(cx, splitter, s)
			if z.IsNull() {
				q = int(advanceStringIndex(s, int64(q), unicodeMatching))
				continue
			}
			end = int(min(e.getLastIndex(cx, splitter), int64(size)))
		}
		if end == p {
			q = int(advanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		out = append(out, cx.Substring(s, p, q))
		if uint32(len(out)) == lim {
			return engine.ObjectValue(cx.NewArray(out))
		}
		p = end
		if fast {
			for i := 1; i < len(caps)/2; i++ {
				out = append(out, capture(cx, s, caps, i))
				if uint32(len(out)) == lim {
					return engine.ObjectValue(cx.NewArray(out))
				}
			}
		} else {
			n := max(cx.LengthOfArrayLike(z.Object())-1, 0)
			for i := int64(1); i <= n; i++ {
				out = append(out, getAt(cx, z.Object(), i))
				if uint32(len(out)) == lim {
					return engine.ObjectValue(cx.NewArray(out))
				}
			}
		}
		q = p
	}
	out = append(out, cx.Substring(s, p, size))
	return engine.ObjectValue(cx.NewArray(out))
}

// substCaptures is the captures of a match for GetSubstitution. They are vals, which holds
// strings and undefined, unless str is set, in which case they are the capture indices caps in str.
type substCaptures struct {
	vals []engine.Value
	str  engine.Value
	caps []int
}

// count returns the number of the captures.
func (c *substCaptures) count() int {
	if c.str.IsUndefined() {
		return len(c.vals)
	}
	return len(c.caps)/2 - 1
}

// get returns the capture i, counted from 1, and whether it is not undefined.
func (c *substCaptures) get(i int) (wtf8.String, bool) {
	if c.str.IsUndefined() {
		v := c.vals[i-1]
		if v.IsUndefined() {
			return "", false
		}
		return v.Str(), true
	}
	a, b := c.caps[2*i], c.caps[2*i+1]
	if a < 0 {
		return "", false
	}
	return c.str.Substring(a, b), true
}

// writeLimited writes s to b, and throws a RangeError instead when b would get longer than
// MaxStringLength.
func writeLimited(cx *engine.ExecutionContext, b *wtf8.Builder, s wtf8.String) {
	if b.Len()+len(s) > engine.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	b.WriteString(s)
}

// writeSubstitution implements GetSubstitution, writing the result to b.
func writeSubstitution(cx *engine.ExecutionContext, b *wtf8.Builder, matched wtf8.String, str engine.Value, position int, captures substCaptures, named engine.Value, template wtf8.String) {
	length := str.StringLength()
	ncaps := captures.count()
	for i := 0; i < len(template); {
		c := template[i]
		if c != '$' || i+1 >= len(template) {
			j := strings.IndexByte(string(template[i+1:]), '$')
			if j < 0 {
				writeLimited(cx, b, template[i:])
				break
			}
			writeLimited(cx, b, template[i:i+1+j])
			i += 1 + j
			continue
		}
		switch next := template[i+1]; {
		case next == '$':
			writeLimited(cx, b, "$")
			i += 2
		case next == '&':
			writeLimited(cx, b, matched)
			i += 2
		case next == '`':
			writeLimited(cx, b, str.Substring(0, position))
			i += 2
		case next == '\'':
			if tail := position + wtf8.Len(matched); tail < length {
				writeLimited(cx, b, str.Substring(tail, length))
			}
			i += 2
		case next >= '0' && next <= '9':
			digits := 1
			if i+2 < len(template) && template[i+2] >= '0' && template[i+2] <= '9' {
				digits = 2
			}
			index, _ := strconv.Atoi(string(template[i+1 : i+1+digits]))
			if index > ncaps && digits == 2 {
				digits = 1
				index = int(next - '0')
			}
			ref := template[i : i+1+digits]
			if index >= 1 && index <= ncaps {
				if c, ok := captures.get(index); ok {
					writeLimited(cx, b, c)
				}
			} else {
				writeLimited(cx, b, ref)
			}
			i += len(ref)
		case next == '<':
			gt := strings.IndexByte(string(template[i:]), '>')
			if gt < 0 || named.IsUndefined() {
				writeLimited(cx, b, "$<")
				i += 2
				continue
			}
			name := template[i+2 : i+gt]
			v := cx.Get(named.Object(), cx.ToPropertyKey(cx.NewString(name)))
			if !v.IsUndefined() {
				writeLimited(cx, b, cx.ToString(v))
			}
			i += gt + 1
		default:
			writeLimited(cx, b, "$")
			i++
		}
	}
}

// regExpEscape implements RegExp.escape, checking for an interruption while it escapes a long
// string.
func regExpEscape(cx *engine.ExecutionContext, s wtf8.String) wtf8.String {
	var b wtf8.Buffer
	hex := func(prefix wtf8.String, v rune, n int) {
		b.WriteString(prefix)
		h := wtf8.String(strconv.FormatInt(int64(v), 16))
		for range n - len(h) {
			b.WriteString("0")
		}
		b.WriteString(h)
	}
	next := escapeRun
	for i := 0; i < len(s); {
		if i >= next {
			checkStringLength(cx, b.Len())
			cx.Poll()
			next = i + escapeRun
		}
		r, size := wtf8.DecodeRune(s[i:])
		first := i == 0
		i += size
		switch {
		case first && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			hex("\\x", r, 2)
		case strings.ContainsRune("^$\\.*+?()[]{}|/", r):
			b.WriteString("\\")
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("\\t")
		case r == '\n':
			b.WriteString("\\n")
		case r == '\v':
			b.WriteString("\\v")
		case r == '\f':
			b.WriteString("\\f")
		case r == '\r':
			b.WriteString("\\r")
		case strings.ContainsRune(",-=<>#&!%:;@~'`\"", r) || xstrings.IsWhiteSpaceOrLineTerminator(r) || r >= 0xD800 && r <= 0xDFFF:
			if r <= 0xFF {
				hex("\\x", r, 2)
			} else {
				hex("\\u", r, 4)
			}
		default:
			b.WriteRune(r)
		}
	}
	checkStringLength(cx, b.Len())
	return b.String()
}

// initLegacyStatics defines the legacy static properties of RegExp.
func initLegacyStatics(cx *engine.ExecutionContext, ctor *engine.Object, env *regexpEnv) {
	l := &env.legacy
	l.input = cx.NewString("")
	l.str = l.input
	str := func(a, b int) engine.Value {
		if a < 0 {
			return cx.NewString("")
		}
		return cx.Substring(l.str, a, b)
	}
	define := func(names []string, matchResult func() engine.Value) {
		slot := &l.input
		if matchResult != nil {
			slot = &l.str
		}
		for _, name := range names {
			check := func(cx *engine.ExecutionContext, this engine.Value, what string) {
				if !this.IsObject() || this.Object() != ctor {
					cx.ThrowTypeError("RegExp." + name + " " + what + " called on an incompatible receiver")
				}
			}
			get := cx.NewNative(wtf8.String("get "+name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
				check(cx, this, "getter")
				if slot.IsUndefined() {
					cx.ThrowTypeError("RegExp." + name + " is not available after a match of a subclass instance")
				}
				if matchResult != nil && len(l.caps) > 0 {
					return matchResult()
				}
				return *slot
			})
			var set *engine.Object
			if matchResult == nil {
				set = cx.NewNative(wtf8.String("set "+name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
					check(cx, this, "setter")
					l.input = cx.ToStringValue(arg(args, 0))
					return engine.Undefined
				})
			}
			cx.DefineAccessor(ctor, cx.Intern(wtf8.String(name)), get, set, engine.FlagConfigurable)
		}
	}
	define([]string{"input", "$_"}, nil)
	define([]string{"lastMatch", "$&"}, func() engine.Value {
		return str(l.caps[0], l.caps[1])
	})
	define([]string{"lastParen", "$+"}, func() engine.Value {
		n := len(l.caps) / 2
		if n <= 1 {
			return cx.NewString("")
		}
		return str(l.caps[2*n-2], l.caps[2*n-1])
	})
	define([]string{"leftContext", "$`"}, func() engine.Value {
		return str(0, l.caps[0])
	})
	define([]string{"rightContext", "$'"}, func() engine.Value {
		return str(l.caps[1], l.str.StringLength())
	})
	for i := 1; i <= 9; i++ {
		define([]string{"$" + strconv.Itoa(i)}, func() engine.Value {
			if 2*i+1 >= len(l.caps) {
				return cx.NewString("")
			}
			return str(l.caps[2*i], l.caps[2*i+1])
		})
	}
}
