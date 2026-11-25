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

// regExpPattern is the pattern that a RegExp object had.
type regExpPattern struct {
	source  wtf8.String
	matcher *regexp.Regexp
}

// unmodifiedPattern returns the pattern of r when [engine.QuietContext.UnmodifiedRegExpMatcher]
// accepts r. It reports false otherwise.
func unmodifiedPattern(q engine.QuietContext, r *engine.Object) (regExpPattern, bool) {
	m := q.UnmodifiedRegExpMatcher(r)
	if m == nil {
		return regExpPattern{}, false
	}
	src, _ := r.RegExpOriginal()
	return regExpPattern{
		source:  src,
		matcher: m,
	}, true
}

// getFlags returns ToString(Get(r, "flags")).
func getFlags(cx *engine.ExecutionContext, r *engine.Object) engine.Value {
	return cx.ToStringValue(cx.Get(r, engine.NamedKey(cx.Key(engine.KeyFlags))))
}

// getMatchFlags reports whether ToString(Get(r, "flags")) contains g, and whether it contains u or v.
func getMatchFlags(cx *engine.ExecutionContext, r *engine.Object) (global, fullUnicode bool) {
	if m := cx.Quiet().UnmodifiedRegExpMatcher(r); m != nil {
		f := m.Flags()
		return f&regexp.FlagGlobal != 0, f&(regexp.FlagUnicode|regexp.FlagUnicodeSets) != 0
	}
	flags := string(getFlags(cx, r).Str())
	return strings.Contains(flags, "g"), strings.ContainsAny(flags, "uv")
}

// getLastIndex returns ToLength(Get(r, "lastIndex")).
func getLastIndex(cx *engine.ExecutionContext, r *engine.Object) int64 {
	v, ok := cx.Quiet().RegExpLastIndex(r)
	if !ok {
		v = cx.Get(r, engine.NamedKey(cx.Key(engine.KeyLastIndex)))
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

func setLastIndex(cx *engine.ExecutionContext, r *engine.Object, n int64) {
	if !cx.Quiet().SetRegExpLastIndex(r, engine.Number(float64(n))) {
		cx.Set(r, engine.NamedKey(cx.Key(engine.KeyLastIndex)), engine.Number(float64(n)))
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

// builtinExecRaw implements RegExpBuiltinExec from the use of lastIndex, which is the value of
// the property converted to a length, up to the creation of the match array.
// It returns the code unit indices of the captures, which are valid until the next match,
// or nil when there is no match.
func builtinExecRaw(cx *engine.ExecutionContext, r *engine.Object, s engine.Value, lastIndex int64) []int {
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
			setLastIndex(cx, r, 0)
		}
		return nil
	}
	caps := cx.Quiet().MatchRegExp(re, s, int(lastIndex))
	if caps == nil {
		if global || sticky {
			setLastIndex(cx, r, 0)
		}
		return nil
	}
	if global || sticky {
		setLastIndex(cx, r, int64(caps[1]))
	}
	cx.Quiet().RecordRegExpMatch(r, s, caps)
	return caps
}

// capture returns the capture i of caps in s, or undefined.
func capture(q engine.QuietContext, s engine.Value, caps []int, i int) engine.Value {
	a, b := caps[2*i], caps[2*i+1]
	if a < 0 {
		return engine.Undefined
	}
	if a == 0 && b == s.StringLength() {
		return s
	}
	return q.Substring(s, a, b)
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
func builtinExec(cx *engine.ExecutionContext, r *engine.Object, s engine.Value) engine.Value {
	// Converting lastIndex can run JavaScript that recompiles r.
	lastIndex := getLastIndex(cx, r)
	caps := builtinExecRaw(cx, r, s, lastIndex)
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
			a.SetElement(int64(i), capture(cx.Quiet(), s, caps, i))
			continue
		}
		start, end := cx.Quiet().RegExpMatchSpan(i)
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
		v, _ := engine.Element(a, int64(i))
		return v
	})
	if pairs == nil {
		cx.DefineMatchResultProperties(a, index, s, groups)
		return engine.ObjectValue(a)
	}
	ia := cx.NewArray(pairs)
	cx.CreateDataPropertyOrThrow(ia, engine.NamedKey(cx.Key(engine.KeyGroups)), groupsObject(cx, re, func(i int) engine.Value { return pairs[i] }))
	cx.DefineMatchResultPropertiesWithIndices(a, index, s, groups, engine.ObjectValue(ia))
	return engine.ObjectValue(a)
}

// execMatch performs RegExpExec, returning capture indices or the result object, or both nil for no match.
func execMatch(cx *engine.ExecutionContext, r *engine.Object, s engine.Value) ([]int, *engine.Object) {
	var exec, index engine.Value
	if cx.Quiet().UnmodifiedRegExpMatcher(r) != nil {
		index, _ = cx.Quiet().RegExpLastIndex(r)
		exec = engine.ObjectValue(cx.Intrinsic(engine.IntrinsicRegExpPrototypeExec))
	} else {
		exec = cx.Get(r, engine.NamedKey(cx.Key(engine.KeyExec)))
		if exec.IsObject() && exec.Object() == cx.Intrinsic(engine.IntrinsicRegExpPrototypeExec) && r.RegExpMatcher() != nil {
			if d, _ := cx.GetOwnProperty(r, engine.NamedKey(cx.Key(engine.KeyLastIndex))); d.Writable() {
				index = d.Value()
			}
		}
	}
	// With a writable numeric lastIndex, the original exec can run without a native frame.
	// Coercion can otherwise run JavaScript, and a read-only lastIndex can throw on a write.
	if index.IsNumber() {
		return builtinExecRaw(cx, r, s, numericLastIndex(index.Number())), nil
	}
	result := regExpExecWith(cx, r, s, exec)
	if result.IsNull() {
		return nil, nil
	}
	return nil, result.Object()
}

// cloneRegExp returns a new RegExp object with the pattern src and the flags flags. It does what
// Construct(%RegExp%, « r, flags ») does for a RegExp object r with the source src for which
// [engine.QuietContext.UnmodifiedRegExpMatcher] returns a matcher.
func cloneRegExp(cx *engine.ExecutionContext, src wtf8.String, flags engine.Value) *engine.Object {
	return cx.RegExpInitialize(cx.RegExpAlloc(cx.Intrinsic(engine.IntrinsicRegExp)), cx.NewString(src), flags)
}

// regExpExecWith implements RegExpExec with the value of r.exec already read.
func regExpExecWith(cx *engine.ExecutionContext, r *engine.Object, s engine.Value, exec engine.Value) engine.Value {
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
	return builtinExec(cx, r, s)
}

// regExpExec implements RegExpExec.
func regExpExec(cx *engine.ExecutionContext, r *engine.Object, s engine.Value) engine.Value {
	exec := cx.Get(r, engine.NamedKey(cx.Key(engine.KeyExec)))
	return regExpExecWith(cx, r, s, exec)
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
	if m := cx.Quiet().UnmodifiedRegExpMatcher(o); m != nil {
		global = m.Flags()&regexp.FlagGlobal != 0
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
	name   string
	key    engine.CommonKey
	flag   regexp.Flags
	getter engine.Native
}{
	{
		name:   "hasIndices",
		key:    engine.KeyHasIndices,
		flag:   regexp.FlagHasIndices,
		getter: engine.RegisterNative(regExpFlagNative("hasIndices", regexp.FlagHasIndices)),
	},
	{
		name:   "global",
		key:    engine.KeyGlobal,
		flag:   regexp.FlagGlobal,
		getter: engine.RegisterNative(regExpFlagNative("global", regexp.FlagGlobal)),
	},
	{
		name:   "ignoreCase",
		key:    engine.KeyIgnoreCase,
		flag:   regexp.FlagIgnoreCase,
		getter: engine.RegisterNative(regExpFlagNative("ignoreCase", regexp.FlagIgnoreCase)),
	},
	{
		name:   "multiline",
		key:    engine.KeyMultiline,
		flag:   regexp.FlagMultiline,
		getter: engine.RegisterNative(regExpFlagNative("multiline", regexp.FlagMultiline)),
	},
	{
		name:   "dotAll",
		key:    engine.KeyDotAll,
		flag:   regexp.FlagDotAll,
		getter: engine.RegisterNative(regExpFlagNative("dotAll", regexp.FlagDotAll)),
	},
	{
		name:   "unicode",
		key:    engine.KeyUnicode,
		flag:   regexp.FlagUnicode,
		getter: engine.RegisterNative(regExpFlagNative("unicode", regexp.FlagUnicode)),
	},
	{
		name:   "unicodeSets",
		key:    engine.KeyUnicodeSets,
		flag:   regexp.FlagUnicodeSets,
		getter: engine.RegisterNative(regExpFlagNative("unicodeSets", regexp.FlagUnicodeSets)),
	},
	{
		name:   "sticky",
		key:    engine.KeySticky,
		flag:   regexp.FlagSticky,
		getter: engine.RegisterNative(regExpFlagNative("sticky", regexp.FlagSticky)),
	},
}

// regExpFlagNative returns the getter of the property name of RegExp.prototype, which tells whether
// a regular expression has flag.
//
//go:noinline
func regExpFlagNative(name string, flag regexp.Flags) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			cx.ThrowTypeError("RegExp.prototype." + name + " getter called on non-object")
		}
		re := this.Object().RegExpMatcher()
		if re == nil {
			if this.Object() == cx.Intrinsic(engine.IntrinsicRegExpPrototype) {
				return engine.Undefined
			}
			cx.ThrowTypeError("RegExp.prototype." + name + " getter called on non-RegExp object")
		}
		return engine.Bool(re.Flags()&flag != 0)
	}
}

func thisRegExp(cx *engine.ExecutionContext, this engine.Value, what string) *engine.Object {
	if !this.IsObject() || this.Object().RegExpMatcher() == nil {
		cx.ThrowTypeError("RegExp.prototype." + what + " requires that 'this' be a RegExp object")
	}
	return this.Object()
}

var (
	nativeRegExpConstructor   = engine.RegisterNative(regExpConstructor)
	nativeRegExpExecMethod    = engine.RegisterNative(regExpExecMethod)
	nativeRegExpTest          = engine.RegisterNative(regExpTest)
	nativeRegExpToString      = engine.RegisterNative(regExpToString)
	nativeRegExpCompile       = engine.RegisterNative(regExpCompile)
	nativeRegExpFlags         = engine.RegisterNative(regExpFlags)
	nativeRegExpSource        = engine.RegisterNative(regExpSource)
	nativeRegExpMatch         = engine.RegisterNative(regExpMatch)
	nativeRegExpMatchAll      = engine.RegisterNative(regExpMatchAll)
	nativeRegExpReplaceMethod = engine.RegisterNative(regExpReplaceMethod)
	nativeRegExpSearch        = engine.RegisterNative(regExpSearch)
	nativeRegExpSplitMethod   = engine.RegisterNative(regExpSplitMethod)
	nativeRegExpEscapeMethod  = engine.RegisterNative(regExpEscapeMethod)
)

func initRegExp(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicRegExpPrototype)
	ctor := constructor(cx, "RegExp", 2, engine.IntrinsicRegExpPrototype, nativeRegExpConstructor)
	cx.SetIntrinsic(engine.IntrinsicRegExp, ctor)
	cx.DefineAccessor(ctor, cx.WellKnownSymbol(engine.SymbolSpecies), cx.NewNative("get [Symbol.species]", 0, nativeReturnThis), nil, engine.FlagConfigurable)

	// exec and test borrow the RegExp object of a literal they are called on. They run no script
	// code before the last use of it, except the conversion of an object argument.
	exec := method(cx, proto, "exec", 1, nativeRegExpExecMethod)
	cx.SetIntrinsic(engine.IntrinsicRegExpPrototypeExec, exec)
	cx.LendRegExpLiteralReceivers(exec)
	cx.LendRegExpLiteralReceivers(method(cx, proto, "test", 1, nativeRegExpTest))
	method(cx, proto, "toString", 0, nativeRegExpToString)
	method(cx, proto, "compile", 2, nativeRegExpCompile)
	for _, fp := range flagProps {
		getter(cx, proto, cx.Key(fp.key), wtf8.String(fp.name), fp.getter)
	}
	getter(cx, proto, cx.Key(engine.KeyFlags), "flags", nativeRegExpFlags)
	getter(cx, proto, cx.Key(engine.KeySource), "source", nativeRegExpSource)

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolMatch), "[Symbol.match]", 1, nativeRegExpMatch)

	iterProto := cx.NewObject(cx.Intrinsic(engine.IntrinsicIteratorPrototype))
	cx.SetIntrinsic(engine.IntrinsicRegExpStringIteratorPrototype, iterProto)
	method(cx, iterProto, "next", 0, nativeRegExpStringIteratorNext)
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("RegExp String Iterator"), engine.FlagConfigurable)

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolMatchAll), "[Symbol.matchAll]", 1, nativeRegExpMatchAll)
	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolReplace), "[Symbol.replace]", 2, nativeRegExpReplaceMethod)
	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolSearch), "[Symbol.search]", 1, nativeRegExpSearch)
	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolSplit), "[Symbol.split]", 2, nativeRegExpSplitMethod)

	method(cx, ctor, "escape", 1, nativeRegExpEscapeMethod)

	initLegacyStatics(cx, ctor)

	// The fast paths read these properties of an unmodified RegExp object without a lookup:
	// the flag accessors, exec, constructor and @@species for SpeciesConstructor, and @@match
	// for the IsRegExp of the RegExp constructor. The String methods that borrow RegExp objects
	// read @@match, @@replace, @@search and @@split without running script code.
	protoKeys := []*engine.Object{
		cx.Key(engine.KeyFlags),
		cx.Key(engine.KeyExec),
		cx.Key(engine.KeyConstructor),
		cx.WellKnownSymbol(engine.SymbolMatch),
		cx.WellKnownSymbol(engine.SymbolReplace),
		cx.WellKnownSymbol(engine.SymbolSearch),
		cx.WellKnownSymbol(engine.SymbolSplit),
	}
	for _, fp := range flagProps {
		protoKeys = append(protoKeys, cx.Key(fp.key))
	}
	cx.RecordRegExpOriginals(protoKeys, []*engine.Object{cx.WellKnownSymbol(engine.SymbolSpecies)})
}

func regExpConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
		p = cx.GetV(pattern, cx.Key(engine.KeySource))
		if flags.IsUndefined() {
			f = cx.GetV(pattern, cx.Key(engine.KeyFlags))
		} else {
			f = flags
		}
	default:
		p, f = pattern, flags
	}
	o := cx.RegExpAlloc(nt)
	return engine.ObjectValue(cx.RegExpInitialize(o, p, f))
}

func regExpExecMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if arg(args, 0).IsObject() {
		this = cx.OwnRegExp(this)
	}
	r := thisRegExp(cx, this, "exec")
	s := cx.ToStringValue(arg(args, 0))
	return builtinExec(cx, r, s)
}

func regExpTest(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if arg(args, 0).IsObject() {
		this = cx.OwnRegExp(this)
	}
	r := thisObject(cx, this, "RegExp.prototype.test")
	s := cx.ToStringValue(arg(args, 0))
	caps, result := execMatch(cx, r, s)
	return engine.Bool(caps != nil || result != nil)
}

func regExpToString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := thisObject(cx, this, "RegExp.prototype.toString")
	src := cx.ToString(cx.Get(r, engine.NamedKey(cx.Key(engine.KeySource))))
	fl := cx.ToString(cx.Get(r, engine.NamedKey(cx.Key(engine.KeyFlags))))
	checkStringLength(cx, len(src)+len(fl)+2)
	return cx.NewString(wtf8.Concat(wtf8.Concat("/"+src, "/"), fl))
}

func regExpCompile(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func regExpFlags(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := thisObject(cx, this, "RegExp.prototype.flags getter")
	if m := cx.Quiet().UnmodifiedRegExpMatcher(r); m != nil {
		return cx.RegExpFlagsString(m.Flags())
	}
	var f regexp.Flags
	for _, fp := range flagProps {
		if engine.ToBoolean(cx.Get(r, engine.NamedKey(cx.Key(fp.key)))) {
			f |= fp.flag
		}
	}
	return cx.RegExpFlagsString(f)
}

func regExpSource(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func regExpMatch(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.match]")
	s := cx.ToStringValue(arg(args, 0))
	global, fullUnicode := getMatchFlags(cx, rx)
	if !global {
		return regExpExec(cx, rx, s)
	}
	setLastIndex(cx, rx, 0)
	var out []engine.Value
	for {
		var matchStr engine.Value
		caps, result := execMatch(cx, rx, s)
		if caps != nil {
			matchStr = capture(cx.Quiet(), s, caps, 0)
		} else if result != nil {
			matchStr = cx.ToStringValue(cx.Get(result, engine.IndexKey(0)))
		} else {
			break
		}
		out = append(out, matchStr)
		if matchStr.Str() == "" {
			i := getLastIndex(cx, rx)
			setLastIndex(cx, rx, advanceStringIndex(s, i, fullUnicode))
		}
	}
	if len(out) == 0 {
		return engine.Null
	}
	return engine.ObjectValue(cx.NewArray(out))
}

// nativeRegExpStringIteratorNext is %RegExpStringIteratorPrototype%.next.
var nativeRegExpStringIteratorNext = engine.RegisterIteratorNext(engine.IteratorMethodRealm, regExpStringIteratorStep)

func regExpStringIteratorStep(cx *engine.ExecutionContext, this engine.Value) (engine.Value, bool) {
	st, ok := engine.IteratorState(this).(*regexpStringIterator)
	if !ok {
		cx.ThrowTypeError("%RegExpStringIteratorPrototype%.next requires that 'this' be a RegExp String Iterator")
	}
	if st.done {
		return engine.Undefined, false
	}
	m := regExpExec(cx, st.r, st.s)
	if m.IsNull() {
		st.done = true
		return engine.Undefined, false
	}
	if !st.global {
		st.done = true
		return m, true
	}
	if cx.ToString(cx.Get(m.Object(), engine.IndexKey(0))) == "" {
		i := getLastIndex(cx, st.r)
		setLastIndex(cx, st.r, advanceStringIndex(st.s, i, st.fullUnicode))
	}
	return m, true
}

func regExpMatchAll(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := thisObject(cx, this, "RegExp.prototype[Symbol.matchAll]")
	s := cx.ToStringValue(arg(args, 0))
	var flags engine.Value
	var matcher *engine.Object
	if p, ok := unmodifiedPattern(cx.Quiet(), r); ok {
		flags = cx.RegExpFlagsString(p.matcher.Flags())
		matcher = cloneRegExp(cx, p.source, flags)
	} else {
		c := speciesConstructor(cx, r, cx.Intrinsic(engine.IntrinsicRegExp))
		flags = getFlags(cx, r)
		matcher = cx.Construct(c, []engine.Value{engine.ObjectValue(r), flags}, c)
	}
	lastIndex := getLastIndex(cx, r)
	setLastIndex(cx, matcher, lastIndex)
	fl := flags.Str()
	return engine.ObjectValue(cx.NewIteratorObject(cx.Intrinsic(engine.IntrinsicRegExpStringIteratorPrototype), &regexpStringIterator{
		r:           matcher,
		s:           s,
		global:      strings.Contains(string(fl), "g"),
		fullUnicode: strings.ContainsAny(string(fl), "uv"),
	}))
}

func regExpReplaceMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.replace]")
	s := cx.ToStringValue(arg(args, 0))
	return regExpReplace(cx, rx, s, arg(args, 1))
}

func regExpSearch(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.search]")
	s := cx.ToStringValue(arg(args, 0))
	key := engine.NamedKey(cx.Key(engine.KeyLastIndex))
	prev := cx.Get(rx, key)
	if !engine.SameValue(prev, engine.Number(0)) {
		cx.Set(rx, key, engine.Number(0))
	}
	index := engine.Number(-1)
	caps, result := execMatch(cx, rx, s)
	if caps != nil {
		index = engine.Number(float64(caps[0]))
	}
	if cur := cx.Get(rx, key); !engine.SameValue(cur, prev) {
		cx.Set(rx, key, prev)
	}
	if result != nil {
		index = cx.Get(result, engine.NamedKey(cx.Key(engine.KeyIndex)))
	}
	return index
}

func regExpSplitMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.split]")
	s := cx.ToStringValue(arg(args, 0))
	return regExpSplit(cx, rx, s, arg(args, 1))
}

func regExpEscapeMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	v := arg(args, 0)
	if !v.IsString() {
		cx.ThrowTypeError("RegExp.escape requires a string")
	}
	return cx.NewString(regExpEscape(cx, v.Str()))
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

func regExpReplace(cx *engine.ExecutionContext, rx *engine.Object, s engine.Value, replaceValue engine.Value) engine.Value {
	functional := replaceValue.IsCallable()
	var template wtf8.String
	if !functional {
		template = cx.ToString(replaceValue)
	}
	global, fullUnicode := getMatchFlags(cx, rx)
	if global {
		setLastIndex(cx, rx, 0)
	}
	length := s.StringLength()
	var b wtf8.Builder
	// next is the index after the last replaced match, or -1 once the result is too long.
	next := 0
	found := false
	// results holds the matches whose replacements are computed after the last match.
	// A match of the original exec with a string template is replaced as soon as it is
	// found, as long as no match is pending, since GetSubstitution then runs no JavaScript.
	var resultsArr [4]match
	var capsArr [16]int
	results, capsBuf := resultsArr[:0], capsArr[:0]
	for {
		caps, res := execMatch(cx, rx, s)
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
				// When the result gets too long, the remaining matches are found before the
				// error is thrown, as they can be observed.
				if next >= 0 {
					next = substituteMatch(cx, &b, re, s, caps, template, next)
				}
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
			i := getLastIndex(cx, rx)
			setLastIndex(cx, rx, advanceStringIndex(s, i, fullUnicode))
		}
	}
	if !found {
		return s
	}
	if next < 0 {
		cx.ThrowRangeError("invalid string length")
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
				captures[i] = capture(cx.Quiet(), s, caps, i+1)
			}
			named = engine.Undefined
			if len(re.Names()) > 0 && (functional || strings.Contains(string(template), "$<")) {
				named = groupsObject(cx, re, func(i int) engine.Value { return captures[i-1] })
			}
		} else {
			res := r.res
			n := max(cx.LengthOfArrayLike(res)-1, 0)
			matched = cx.ToString(cx.Get(res, engine.IndexKey(0)))
			p := cx.ToIntegerOrInfinity(cx.Get(res, engine.NamedKey(cx.Key(engine.KeyIndex))))
			position = int(math.Max(math.Min(p, float64(length)), 0))
			captures = make([]engine.Value, n)
			for i := range captures {
				c := getAt(cx, res, int64(i)+1)
				if !c.IsUndefined() {
					c = cx.ToStringValue(c)
				}
				captures[i] = c
			}
			named = cx.Get(res, engine.NamedKey(cx.Key(engine.KeyGroups)))
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
// indices caps, followed by its replacement by template. It returns the index after the match,
// or -1 when b would get longer than MaxStringLength.
func substituteMatch(cx *engine.ExecutionContext, b *wtf8.Builder, re *regexp.Regexp, s engine.Value, caps []int, template wtf8.String, next int) int {
	position := caps[0]
	if position < next {
		return next
	}
	if !tryWrite(b, s.Substring(next, position)) {
		return -1
	}
	matched := s.Substring(caps[0], caps[1])
	if !strings.Contains(string(template), "$") {
		if !tryWrite(b, template) {
			return -1
		}
		return caps[1]
	}
	named := engine.Undefined
	if len(re.Names()) > 0 && strings.Contains(string(template), "$<") {
		named = groupsObject(cx, re, func(i int) engine.Value { return capture(cx.Quiet(), s, caps, i) })
	}
	if !tryWriteSubstitution(cx, b, matched, s, position, substCaptures{str: s, caps: caps}, named, template) {
		return -1
	}
	return caps[1]
}

func regExpSplit(cx *engine.ExecutionContext, rx *engine.Object, s engine.Value, limit engine.Value) engine.Value {
	// For an unmodified rx, finding the constructor and constructing the splitter run no
	// JavaScript, so the splitter is only made when the algorithm can observe it.
	pattern, unmodified := unmodifiedPattern(cx.Quiet(), rx)
	var c *engine.Object
	var splitter *engine.Object
	var unicodeMatching bool
	if unmodified {
		c = cx.Intrinsic(engine.IntrinsicRegExp)
		unicodeMatching = pattern.matcher.Flags()&(regexp.FlagUnicode|regexp.FlagUnicodeSets) != 0
	} else {
		c = speciesConstructor(cx, rx, cx.Intrinsic(engine.IntrinsicRegExp))
		flags := getFlags(cx, rx).Str()
		unicodeMatching = strings.ContainsAny(string(flags), "uv")
		newFlags := flags
		if !strings.Contains(string(flags), "y") {
			newFlags += "y"
		}
		splitter = cx.Construct(c, []engine.Value{engine.ObjectValue(rx), cx.NewString(newFlags)}, c)
	}
	lim := uint32(math.MaxUint32)
	if !limit.IsUndefined() {
		lim = cx.ToUint32(limit)
	}
	if lim == 0 {
		return engine.ObjectValue(cx.NewArray(nil))
	}
	// When the splitter is a RegExp object that no code can see, and its exec is the original
	// one, searching for the next match is the same as trying every position. The matcher of rx
	// finds the same matches as that of the splitter, which differs only in the sticky flag.
	if splitter == nil {
		if v, ok := splitUnmodified(cx.Quiet(), rx, pattern.matcher, s, lim, unicodeMatching); ok {
			return v
		}
		// ToUint32 changed rx or %RegExp.prototype%.
		splitter = cloneRegExp(cx, pattern.source, cx.RegExpFlagsString(pattern.matcher.Flags()|regexp.FlagSticky))
	}
	// Reading exec from a splitter that is not unmodified can run JavaScript, so it is read
	// only where RegExpExec reads it.
	if c == cx.Intrinsic(engine.IntrinsicRegExp) {
		if v, ok := splitUnmodified(cx.Quiet(), splitter, splitter.RegExpMatcher(), s, lim, unicodeMatching); ok {
			return v
		}
	}
	size := s.StringLength()
	if size == 0 {
		caps, result := execMatch(cx, splitter, s)
		if caps != nil || result != nil {
			return engine.ObjectValue(cx.NewArray(nil))
		}
		return engine.ObjectValue(cx.NewArray([]engine.Value{s}))
	}
	var out []engine.Value
	p, q := 0, 0
	for q < size {
		setLastIndex(cx, splitter, int64(q))
		z := regExpExec(cx, splitter, s)
		if z.IsNull() {
			q = int(advanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		end := int(min(getLastIndex(cx, splitter), int64(size)))
		if end == p {
			q = int(advanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		out = append(out, cx.Substring(s, p, q))
		if uint32(len(out)) == lim {
			return engine.ObjectValue(cx.NewArray(out))
		}
		p = end
		n := max(cx.LengthOfArrayLike(z.Object())-1, 0)
		for i := int64(1); i <= n; i++ {
			out = append(out, getAt(cx, z.Object(), i))
			if uint32(len(out)) == lim {
				return engine.ObjectValue(cx.NewArray(out))
			}
		}
		q = p
	}
	out = append(out, cx.Substring(s, p, size))
	return engine.ObjectValue(cx.NewArray(out))
}

// splitUnmodified returns the array of at most lim strings that splitting s by the matches of re
// makes, when [engine.QuietContext.UnmodifiedRegExpMatcher] accepts r. It reports false otherwise.
func splitUnmodified(qx engine.QuietContext, r *engine.Object, re *regexp.Regexp, s engine.Value, lim uint32, unicodeMatching bool) (engine.Value, bool) {
	if qx.UnmodifiedRegExpMatcher(r) == nil {
		return engine.Undefined, false
	}
	size := s.StringLength()
	if size == 0 {
		// The splitter is sticky and its lastIndex is 0, so it can only match at 0.
		if caps := qx.FindRegExp(re, s, 0); caps != nil {
			qx.UpdateLegacyRegExpStaticProperties(s, caps)
			return engine.ObjectValue(qx.NewArray(nil)), true
		}
		return engine.ObjectValue(qx.NewArray([]engine.Value{s})), true
	}
	var out []engine.Value
	p, q := 0, 0
	for q < size {
		caps := qx.FindRegExp(re, s, q)
		if caps == nil || caps[0] >= size {
			break
		}
		qx.UpdateLegacyRegExpStaticProperties(s, caps)
		q = caps[0]
		end := caps[1]
		if end == p {
			q = int(advanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		out = append(out, qx.Substring(s, p, q))
		if uint32(len(out)) == lim {
			return engine.ObjectValue(qx.NewArray(out)), true
		}
		p = end
		for i := 1; i < len(caps)/2; i++ {
			out = append(out, capture(qx, s, caps, i))
			if uint32(len(out)) == lim {
				return engine.ObjectValue(qx.NewArray(out)), true
			}
		}
		q = p
	}
	out = append(out, qx.Substring(s, p, size))
	return engine.ObjectValue(qx.NewArray(out)), true
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

// tryWrite writes s to b, or reports false when b would get longer than MaxStringLength.
func tryWrite(b *wtf8.Builder, s wtf8.String) bool {
	if b.Len()+len(s) > engine.MaxStringLength {
		return false
	}
	b.WriteString(s)
	return true
}

// writeSubstitution implements GetSubstitution, writing the result to b. It throws a RangeError
// when b would get longer than MaxStringLength.
func writeSubstitution(cx *engine.ExecutionContext, b *wtf8.Builder, matched wtf8.String, str engine.Value, position int, captures substCaptures, named engine.Value, template wtf8.String) {
	if !tryWriteSubstitution(cx, b, matched, str, position, captures, named, template) {
		cx.ThrowRangeError("invalid string length")
	}
}

// tryWriteSubstitution is writeSubstitution, but reports false instead of throwing when b would
// get longer than MaxStringLength.
func tryWriteSubstitution(cx *engine.ExecutionContext, b *wtf8.Builder, matched wtf8.String, str engine.Value, position int, captures substCaptures, named engine.Value, template wtf8.String) bool {
	length := str.StringLength()
	ncaps := captures.count()
	for i := 0; i < len(template); {
		c := template[i]
		if c != '$' || i+1 >= len(template) {
			j := strings.IndexByte(string(template[i+1:]), '$')
			if j < 0 {
				if !tryWrite(b, template[i:]) {
					return false
				}
				break
			}
			if !tryWrite(b, template[i:i+1+j]) {
				return false
			}
			i += 1 + j
			continue
		}
		switch next := template[i+1]; {
		case next == '$':
			if !tryWrite(b, "$") {
				return false
			}
			i += 2
		case next == '&':
			if !tryWrite(b, matched) {
				return false
			}
			i += 2
		case next == '`':
			if !tryWrite(b, str.Substring(0, position)) {
				return false
			}
			i += 2
		case next == '\'':
			if tail := position + wtf8.Len(matched); tail < length {
				if !tryWrite(b, str.Substring(tail, length)) {
					return false
				}
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
					if !tryWrite(b, c) {
						return false
					}
				}
			} else {
				if !tryWrite(b, ref) {
					return false
				}
			}
			i += len(ref)
		case next == '<':
			gt := strings.IndexByte(string(template[i:]), '>')
			if gt < 0 || named.IsUndefined() {
				if !tryWrite(b, "$<") {
					return false
				}
				i += 2
				continue
			}
			name := template[i+2 : i+gt]
			v := cx.Get(named.Object(), cx.ToPropertyKey(cx.NewString(name)))
			if !v.IsUndefined() {
				if !tryWrite(b, cx.ToString(v)) {
					return false
				}
			}
			i += gt + 1
		default:
			if !tryWrite(b, "$") {
				return false
			}
			i++
		}
	}
	return true
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

// legacyStatic is a legacy static property of RegExp. Its accessors share it as their state.
type legacyStatic struct {
	name string

	// span returns the range of the value in the subject s of the last match, whose capture
	// indices are caps. The start is -1 for an empty value. span is nil for input.
	span func(s engine.Value, caps []int, group int) (start, end int)

	// group is the capturing group that span gets.
	group int
}

func lastMatchSpan(s engine.Value, caps []int, group int) (start, end int) {
	return caps[0], caps[1]
}

func lastParenSpan(s engine.Value, caps []int, group int) (start, end int) {
	return groupSpan(s, caps, len(caps)/2-1)
}

func leftContextSpan(s engine.Value, caps []int, group int) (start, end int) {
	return 0, caps[0]
}

func rightContextSpan(s engine.Value, caps []int, group int) (start, end int) {
	return caps[1], s.StringLength()
}

func groupSpan(s engine.Value, caps []int, group int) (start, end int) {
	if group < 1 || 2*group+1 >= len(caps) {
		return -1, -1
	}
	return caps[2*group], caps[2*group+1]
}

func throwIncompatibleLegacyStatic(cx *engine.ExecutionContext, p *legacyStatic, what string) {
	cx.ThrowTypeError("RegExp." + p.name + " " + what + " called on an incompatible receiver")
}

func throwUnavailableLegacyStatic(cx *engine.ExecutionContext, p *legacyStatic) {
	cx.ThrowTypeError("RegExp." + p.name + " is not available after a match of a subclass instance")
}

func legacyInputGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	p := cx.ActiveNativeState().(*legacyStatic)
	if !this.IsObject() || this.Object() != cx.Intrinsic(engine.IntrinsicRegExp) {
		throwIncompatibleLegacyStatic(cx, p, "getter")
	}
	v := cx.LegacyRegExpInput()
	if v.IsUndefined() {
		throwUnavailableLegacyStatic(cx, p)
	}
	return v
}

func legacyInputSetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	p := cx.ActiveNativeState().(*legacyStatic)
	if !this.IsObject() || this.Object() != cx.Intrinsic(engine.IntrinsicRegExp) {
		throwIncompatibleLegacyStatic(cx, p, "setter")
	}
	cx.SetLegacyRegExpInput(cx.ToStringValue(arg(args, 0)))
	return engine.Undefined
}

// legacyMatchGetter is the getter of the properties other than input.
func legacyMatchGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	p := cx.ActiveNativeState().(*legacyStatic)
	if !this.IsObject() || this.Object() != cx.Intrinsic(engine.IntrinsicRegExp) {
		throwIncompatibleLegacyStatic(cx, p, "getter")
	}
	s, caps := cx.LegacyRegExpMatch()
	if s.IsUndefined() {
		throwUnavailableLegacyStatic(cx, p)
	}
	if len(caps) == 0 {
		return s
	}
	start, end := p.span(s, caps, p.group)
	if start < 0 {
		return cx.NewString("")
	}
	return cx.Substring(s, start, end)
}

var (
	nativeLegacyInputSetter = engine.RegisterNative(legacyInputSetter)
	nativeLegacyInputGetter = engine.RegisterNative(legacyInputGetter)
	nativeLegacyMatchGetter = engine.RegisterNative(legacyMatchGetter)
)

// initLegacyStatics defines the legacy static properties of RegExp.
func initLegacyStatics(cx *engine.ExecutionContext, ctor *engine.Object) {
	cx.Quiet().UpdateLegacyRegExpStaticProperties(cx.NewString(""), nil)
	define := func(p legacyStatic) {
		name := wtf8.String(p.name)
		getter := nativeLegacyMatchGetter
		var set *engine.Object
		if p.span == nil {
			getter = nativeLegacyInputGetter
			set = cx.NewNativeWithState("set "+name, 1, nativeLegacyInputSetter, &p)
		}
		get := cx.NewNativeWithState("get "+name, 0, getter, &p)
		cx.DefineAccessor(ctor, cx.Intern(name), get, set, engine.FlagConfigurable)
	}
	for _, p := range []legacyStatic{
		{
			name: "input",
		},
		{
			name: "$_",
		},
		{
			name: "lastMatch",
			span: lastMatchSpan,
		},
		{
			name: "$&",
			span: lastMatchSpan,
		},
		{
			name: "lastParen",
			span: lastParenSpan,
		},
		{
			name: "$+",
			span: lastParenSpan,
		},
		{
			name: "leftContext",
			span: leftContextSpan,
		},
		{
			name: "$`",
			span: leftContextSpan,
		},
		{
			name: "rightContext",
			span: rightContextSpan,
		},
		{
			name: "$'",
			span: rightContextSpan,
		},
	} {
		define(p)
	}
	for i := 1; i <= 9; i++ {
		define(legacyStatic{
			name:  "$" + strconv.Itoa(i),
			span:  groupSpan,
			group: i,
		})
	}
}
