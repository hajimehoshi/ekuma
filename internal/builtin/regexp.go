// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// getFlags returns ToString(Get(r, "flags")).
func getFlags(cx *engine.ExecutionContext, r *storage.Object) storage.Value {
	return cx.ToStringValue(cx.Get(r, storage.NamedKey(cx.Key(storage.KeyFlags))))
}

// getMatchFlags reports whether ToString(Get(r, "flags")) contains g, and whether it contains u or v.
func getMatchFlags(cx *engine.ExecutionContext, r *storage.Object) (global, fullUnicode bool) {
	if m := cx.Quiet().UnmodifiedRegExpMatcher(r); m != nil {
		f := m.Flags()
		return f&regexp.FlagGlobal != 0, f&(regexp.FlagUnicode|regexp.FlagUnicodeSets) != 0
	}
	flags := string(getFlags(cx, r).Str())
	return strings.Contains(flags, "g"), strings.ContainsAny(flags, "uv")
}

// getLastIndex returns ToLength(Get(r, "lastIndex")).
func getLastIndex(cx *engine.ExecutionContext, r *storage.Object) int64 {
	v, ok := cx.Quiet().RegExpLastIndex(r)
	if !ok {
		v = cx.Get(r, storage.NamedKey(cx.Key(storage.KeyLastIndex)))
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

func setLastIndex(cx *engine.ExecutionContext, r *storage.Object, n int64) {
	if !cx.Quiet().SetRegExpLastIndex(r, storage.Number(float64(n))) {
		cx.Set(r, storage.NamedKey(cx.Key(storage.KeyLastIndex)), storage.Number(float64(n)))
	}
}

// builtinExecRaw implements RegExpBuiltinExec from the use of lastIndex, which is the value of
// the property converted to a length, up to the creation of the match array.
// It returns the code unit indices of the captures, which are valid until the next match,
// or nil when there is no match.
func builtinExecRaw(cx *engine.ExecutionContext, r *storage.Object, s storage.Value, lastIndex int64) []int {
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

// groupsObject returns the groups object of a match of re, or undefined when re has no named groups.
func groupsObject(cx *engine.ExecutionContext, re *regexp.Regexp, vals func(i int) storage.Value) storage.Value {
	names := re.Names()
	if len(names) == 0 {
		return storage.Undefined
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
		cx.CreateDataPropertyOrThrow(g, storage.NamedKey(cx.Intern(name)), v)
	}
	return storage.ObjectValue(g)
}

// builtinExec implements RegExpBuiltinExec.
func builtinExec(cx *engine.ExecutionContext, r *storage.Object, s storage.Value) storage.Value {
	// Converting lastIndex can run JavaScript that recompiles r.
	lastIndex := getLastIndex(cx, r)
	caps := builtinExecRaw(cx, r, s, lastIndex)
	if caps == nil {
		return storage.Null
	}
	re := r.RegExpMatcher()
	n := re.GroupCount()
	index := storage.Number(float64(caps[0]))
	nprops := 3
	if re.Flags()&regexp.FlagHasIndices != 0 {
		nprops = 4
	} else if n == 0 {
		_, matched, literal := r.RegExpLiteral()
		if !literal {
			matched = cx.Quiet().MatchCapture(s, caps, 0)
		}
		return storage.ObjectValue(cx.NewLiteralMatchResult(matched, index, s))
	}
	a := cx.NewArrayWithRoom(n+1, nprops)
	for i := 0; i <= n; i++ {
		a.SetElement(int64(i), cx.Quiet().MatchCapture(s, caps, i))
	}
	var pairs []storage.Value
	if re.Flags()&regexp.FlagHasIndices != 0 {
		pairs = make([]storage.Value, n+1)
		for i := range pairs {
			if caps[2*i] >= 0 {
				pairs[i] = storage.ObjectValue(cx.NewArrayPair(
					storage.Number(float64(caps[2*i])),
					storage.Number(float64(caps[2*i+1])),
				))
			}
		}
	}
	groups := groupsObject(cx, re, func(i int) storage.Value {
		v, _ := storage.Element(a, int64(i))
		return v
	})
	if pairs == nil {
		cx.DefineMatchResultProperties(a, index, s, groups)
		return storage.ObjectValue(a)
	}
	ia := cx.NewArray(pairs)
	cx.CreateDataPropertyOrThrow(ia, storage.NamedKey(cx.Key(storage.KeyGroups)), groupsObject(cx, re, func(i int) storage.Value { return pairs[i] }))
	cx.DefineMatchResultPropertiesWithIndices(a, index, s, groups, storage.ObjectValue(ia))
	return storage.ObjectValue(a)
}

// execMatch performs RegExpExec, returning capture indices or the result object, or both nil for no match.
func execMatch(cx *engine.ExecutionContext, r *storage.Object, s storage.Value) ([]int, *storage.Object) {
	var exec, index storage.Value
	if cx.Quiet().UnmodifiedRegExpMatcher(r) != nil {
		index, _ = cx.Quiet().RegExpLastIndex(r)
		exec = storage.ObjectValue(cx.Intrinsic(storage.IntrinsicRegExpPrototypeExec))
	} else {
		// The callers execute r once per match.
		cx.PollStep()
		exec = cx.Get(r, storage.NamedKey(cx.Key(storage.KeyExec)))
		if exec.IsObject() && exec.Object() == cx.Intrinsic(storage.IntrinsicRegExpPrototypeExec) && r.RegExpMatcher() != nil {
			if d, _ := cx.GetOwnProperty(r, storage.NamedKey(cx.Key(storage.KeyLastIndex))); d.Writable() {
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
// [quiet.Context.UnmodifiedRegExpMatcher] returns a matcher.
func cloneRegExp(cx *engine.ExecutionContext, src wtf8.String, flags storage.Value) *storage.Object {
	return cx.RegExpInitialize(cx.RegExpAlloc(cx.Intrinsic(storage.IntrinsicRegExp)), cx.NewString(src), flags)
}

// regExpExecWith implements RegExpExec with the value of r.exec already read.
func regExpExecWith(cx *engine.ExecutionContext, r *storage.Object, s storage.Value, exec storage.Value) storage.Value {
	if exec.IsCallable() {
		res := cx.Call(exec.Object(), storage.ObjectValue(r), s)
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
func regExpExec(cx *engine.ExecutionContext, r *storage.Object, s storage.Value) storage.Value {
	// The caller may execute r many times.
	cx.PollStep()
	exec := cx.Get(r, storage.NamedKey(cx.Key(storage.KeyExec)))
	return regExpExecWith(cx, r, s, exec)
}

// isRegExp implements IsRegExp.
func isRegExp(cx *engine.ExecutionContext, v storage.Value) bool {
	if !v.IsObject() {
		return false
	}
	if m := cx.GetV(v, cx.WellKnownSymbol(storage.SymbolMatch)); !m.IsUndefined() {
		return storage.ToBoolean(m)
	}
	return v.Object().IsRegExpObject()
}

// requireGlobalRegExp throws the TypeError of String.prototype[name] when o is a RegExp
// object, by IsRegExp, whose flags do not include g.
func requireGlobalRegExp(cx *engine.ExecutionContext, o *storage.Object, name string) {
	var global bool
	if m := cx.Quiet().UnmodifiedRegExpMatcher(o); m != nil {
		global = m.Flags()&regexp.FlagGlobal != 0
	} else {
		if !isRegExp(cx, storage.ObjectValue(o)) {
			return
		}
		flags := cx.GetV(storage.ObjectValue(o), cx.Intern("flags"))
		cx.RequireObjectCoercible(flags, "String.prototype."+name)
		global = strings.Contains(string(cx.ToString(flags)), "g")
	}
	if !global {
		cx.ThrowTypeError(name + " must be called with a global RegExp")
	}
}

// regExpCreate implements RegExpCreate.
func regExpCreate(cx *engine.ExecutionContext, p, f storage.Value) *storage.Object {
	return cx.RegExpInitialize(cx.RegExpAlloc(cx.Intrinsic(storage.IntrinsicRegExp)), p, f)
}

// speciesConstructor implements SpeciesConstructor.
func speciesConstructor(cx *engine.ExecutionContext, o *storage.Object, def *storage.Object) *storage.Object {
	c := cx.Get(o, storage.NamedKey(cx.Key(storage.KeyConstructor)))
	if c.IsUndefined() {
		return def
	}
	if !c.IsObject() {
		cx.ThrowTypeError("object.constructor is not an object")
	}
	s := cx.GetV(c, cx.WellKnownSymbol(storage.SymbolSpecies))
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
	key    storage.CommonKey
	flag   regexp.Flags
	getter storage.Native
}{
	{
		name:   "hasIndices",
		key:    storage.KeyHasIndices,
		flag:   regexp.FlagHasIndices,
		getter: engine.RegisterNative(regExpFlagNative("hasIndices", regexp.FlagHasIndices)),
	},
	{
		name:   "global",
		key:    storage.KeyGlobal,
		flag:   regexp.FlagGlobal,
		getter: engine.RegisterNative(regExpFlagNative("global", regexp.FlagGlobal)),
	},
	{
		name:   "ignoreCase",
		key:    storage.KeyIgnoreCase,
		flag:   regexp.FlagIgnoreCase,
		getter: engine.RegisterNative(regExpFlagNative("ignoreCase", regexp.FlagIgnoreCase)),
	},
	{
		name:   "multiline",
		key:    storage.KeyMultiline,
		flag:   regexp.FlagMultiline,
		getter: engine.RegisterNative(regExpFlagNative("multiline", regexp.FlagMultiline)),
	},
	{
		name:   "dotAll",
		key:    storage.KeyDotAll,
		flag:   regexp.FlagDotAll,
		getter: engine.RegisterNative(regExpFlagNative("dotAll", regexp.FlagDotAll)),
	},
	{
		name:   "unicode",
		key:    storage.KeyUnicode,
		flag:   regexp.FlagUnicode,
		getter: engine.RegisterNative(regExpFlagNative("unicode", regexp.FlagUnicode)),
	},
	{
		name:   "unicodeSets",
		key:    storage.KeyUnicodeSets,
		flag:   regexp.FlagUnicodeSets,
		getter: engine.RegisterNative(regExpFlagNative("unicodeSets", regexp.FlagUnicodeSets)),
	},
	{
		name:   "sticky",
		key:    storage.KeySticky,
		flag:   regexp.FlagSticky,
		getter: engine.RegisterNative(regExpFlagNative("sticky", regexp.FlagSticky)),
	},
}

// regExpFlagNative returns the getter of the property name of RegExp.prototype, which tells whether
// a regular expression has flag.
//
//go:noinline
func regExpFlagNative(name string, flag regexp.Flags) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		if !this.IsObject() {
			cx.ThrowTypeError("RegExp.prototype." + name + " getter called on non-object")
		}
		re := this.Object().RegExpMatcher()
		if re == nil {
			if this.Object() == cx.Intrinsic(storage.IntrinsicRegExpPrototype) {
				return storage.Undefined
			}
			cx.ThrowTypeError("RegExp.prototype." + name + " getter called on non-RegExp object")
		}
		return storage.Bool(re.Flags()&flag != 0)
	}
}

func thisRegExp(cx *engine.ExecutionContext, this storage.Value, what string) *storage.Object {
	if !this.IsObject() || this.Object().RegExpMatcher() == nil {
		cx.ThrowTypeError("RegExp.prototype." + what + " requires that 'this' be a RegExp object")
	}
	return this.Object()
}

var (
	nativeRegExpConstructor = engine.RegisterNative(regExpConstructor)
	nativeRegExpExecMethod  = engine.RegisterNativeWithKernels(regExpExecMethod, engine.Kernels{
		Method1:  quiet.RegExpExecKernel,
		Contract: shortContract,
	})
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
	proto := cx.Intrinsic(storage.IntrinsicRegExpPrototype)
	ctor := constructor(cx, "RegExp", 2, storage.IntrinsicRegExpPrototype, nativeRegExpConstructor)
	cx.SetIntrinsic(storage.IntrinsicRegExp, ctor)
	cx.DefineAccessor(ctor, cx.WellKnownSymbol(storage.SymbolSpecies), cx.NewNative("get [Symbol.species]", 0, nativeReturnThis), nil, storage.FlagConfigurable)

	// exec and test borrow the RegExp object of a literal they are called on. They run no script
	// code before the last use of it, except the conversion of an object argument.
	exec := method(cx, proto, "exec", 1, nativeRegExpExecMethod)
	cx.SetIntrinsic(storage.IntrinsicRegExpPrototypeExec, exec)
	cx.LendRegExpLiteralReceivers(exec)
	cx.LendRegExpLiteralReceivers(method(cx, proto, "test", 1, nativeRegExpTest))
	method(cx, proto, "toString", 0, nativeRegExpToString)
	method(cx, proto, "compile", 2, nativeRegExpCompile)
	for _, fp := range flagProps {
		getter(cx, proto, cx.Key(fp.key), wtf8.String(fp.name), fp.getter)
	}
	getter(cx, proto, cx.Key(storage.KeyFlags), "flags", nativeRegExpFlags)
	getter(cx, proto, cx.Key(storage.KeySource), "source", nativeRegExpSource)

	symMethod(cx, proto, cx.WellKnownSymbol(storage.SymbolMatch), "[Symbol.match]", 1, nativeRegExpMatch)

	iterProto := cx.NewObject(cx.Intrinsic(storage.IntrinsicIteratorPrototype))
	cx.SetIntrinsic(storage.IntrinsicRegExpStringIteratorPrototype, iterProto)
	method(cx, iterProto, "next", 0, nativeRegExpStringIteratorNext)
	cx.Define(iterProto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("RegExp String Iterator"), storage.FlagConfigurable)

	symMethod(cx, proto, cx.WellKnownSymbol(storage.SymbolMatchAll), "[Symbol.matchAll]", 1, nativeRegExpMatchAll)
	symMethod(cx, proto, cx.WellKnownSymbol(storage.SymbolReplace), "[Symbol.replace]", 2, nativeRegExpReplaceMethod)
	symMethod(cx, proto, cx.WellKnownSymbol(storage.SymbolSearch), "[Symbol.search]", 1, nativeRegExpSearch)
	symMethod(cx, proto, cx.WellKnownSymbol(storage.SymbolSplit), "[Symbol.split]", 2, nativeRegExpSplitMethod)

	method(cx, ctor, "escape", 1, nativeRegExpEscapeMethod)

	initLegacyStatics(cx, ctor)

	// The fast paths read these properties of an unmodified RegExp object without a lookup:
	// the flag accessors, exec, constructor and @@species for SpeciesConstructor, and @@match
	// for the IsRegExp of the RegExp constructor. The String methods that borrow RegExp objects
	// read @@match, @@replace, @@search and @@split without running script code.
	protoKeys := []*storage.Object{
		cx.Key(storage.KeyFlags),
		cx.Key(storage.KeyExec),
		cx.Key(storage.KeyConstructor),
		cx.WellKnownSymbol(storage.SymbolMatch),
		cx.WellKnownSymbol(storage.SymbolReplace),
		cx.WellKnownSymbol(storage.SymbolSearch),
		cx.WellKnownSymbol(storage.SymbolSplit),
	}
	for _, fp := range flagProps {
		protoKeys = append(protoKeys, cx.Key(fp.key))
	}
	cx.RecordRegExpOriginals(protoKeys, []*storage.Object{cx.WellKnownSymbol(storage.SymbolSpecies)})
}

func regExpConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	pattern, flags := arg(args, 0), arg(args, 1)
	patternIsRegExp := isRegExp(cx, pattern)
	nt := cx.GetNewTarget()
	if nt == nil {
		nt = cx.Intrinsic(storage.IntrinsicRegExp)
		if patternIsRegExp && flags.IsUndefined() {
			pc := cx.GetV(pattern, cx.Key(storage.KeyConstructor))
			if pc.IsObject() && pc.Object() == nt {
				return pattern
			}
		}
	}
	var p, f storage.Value
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
		p = cx.GetV(pattern, cx.Key(storage.KeySource))
		if flags.IsUndefined() {
			f = cx.GetV(pattern, cx.Key(storage.KeyFlags))
		} else {
			f = flags
		}
	default:
		p, f = pattern, flags
	}
	o := cx.RegExpAlloc(nt)
	return storage.ObjectValue(cx.RegExpInitialize(o, p, f))
}

func regExpExecMethod(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	if arg(args, 0).IsObject() {
		this = cx.OwnRegExp(this)
	}
	r := thisRegExp(cx, this, "exec")
	s := cx.ToStringValue(arg(args, 0))
	return builtinExec(cx, r, s)
}

func regExpTest(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	if arg(args, 0).IsObject() {
		this = cx.OwnRegExp(this)
	}
	r := thisObject(cx, this, "RegExp.prototype.test")
	s := cx.ToStringValue(arg(args, 0))
	caps, result := execMatch(cx, r, s)
	return storage.Bool(caps != nil || result != nil)
}

func regExpToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	r := thisObject(cx, this, "RegExp.prototype.toString")
	src := cx.ToString(cx.Get(r, storage.NamedKey(cx.Key(storage.KeySource))))
	fl := cx.ToString(cx.Get(r, storage.NamedKey(cx.Key(storage.KeyFlags))))
	checkStringLength(cx, len(src)+len(fl)+2)
	return cx.NewString(wtf8.Concat(wtf8.Concat("/"+src, "/"), fl))
}

func regExpCompile(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
	return storage.ObjectValue(cx.RegExpInitialize(o, p, f))
}

func regExpFlags(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	r := thisObject(cx, this, "RegExp.prototype.flags getter")
	if m := cx.Quiet().UnmodifiedRegExpMatcher(r); m != nil {
		return cx.RegExpFlagsString(m.Flags())
	}
	var f regexp.Flags
	for _, fp := range flagProps {
		if storage.ToBoolean(cx.Get(r, storage.NamedKey(cx.Key(fp.key)))) {
			f |= fp.flag
		}
	}
	return cx.RegExpFlagsString(f)
}

func regExpSource(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	if !this.IsObject() {
		cx.ThrowTypeError("RegExp.prototype.source getter called on non-object")
	}
	o := this.Object()
	if o.RegExpMatcher() == nil {
		if o == cx.Intrinsic(storage.IntrinsicRegExpPrototype) {
			return cx.NewString("(?:)")
		}
		cx.ThrowTypeError("RegExp.prototype.source getter called on non-RegExp object")
	}
	src, _ := o.RegExpOriginal()
	escaped := escapeRegExpPattern(src)
	checkStringLength(cx, len(escaped))
	return cx.NewString(escaped)
}

func regExpMatch(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.match]")
	s := cx.ToStringValue(arg(args, 0))
	global, fullUnicode := getMatchFlags(cx, rx)
	if !global {
		return regExpExec(cx, rx, s)
	}
	setLastIndex(cx, rx, 0)
	var out []storage.Value
	for {
		var matchStr storage.Value
		caps, result := execMatch(cx, rx, s)
		if caps != nil {
			matchStr = quiet.Capture(cx.Quiet(), s, caps, 0)
		} else if result != nil {
			matchStr = cx.ToStringValue(cx.Get(result, storage.IndexKey(0)))
		} else {
			break
		}
		out = append(out, matchStr)
		if matchStr.Str() == "" {
			i := getLastIndex(cx, rx)
			setLastIndex(cx, rx, quiet.AdvanceStringIndex(s, i, fullUnicode))
		}
	}
	if len(out) == 0 {
		return storage.Null
	}
	return storage.ObjectValue(cx.NewArray(out))
}

// nativeRegExpStringIteratorNext is %RegExpStringIteratorPrototype%.next.
var nativeRegExpStringIteratorNext = engine.RegisterIteratorNext(engine.IteratorMethodRealm, regExpStringIteratorStep)

func regExpStringIteratorStep(cx *engine.ExecutionContext, this storage.Value) (storage.Value, bool) {
	st, ok := storage.IteratorState(this).(*regexpStringIterator)
	if !ok {
		cx.ThrowTypeError("%RegExpStringIteratorPrototype%.next requires that 'this' be a RegExp String Iterator")
	}
	if st.done {
		return storage.Undefined, false
	}
	m := regExpExec(cx, st.r, st.s)
	if m.IsNull() {
		st.done = true
		return storage.Undefined, false
	}
	if !st.global {
		st.done = true
		return m, true
	}
	if cx.ToString(cx.Get(m.Object(), storage.IndexKey(0))) == "" {
		i := getLastIndex(cx, st.r)
		setLastIndex(cx, st.r, quiet.AdvanceStringIndex(st.s, i, st.fullUnicode))
	}
	return m, true
}

func regExpMatchAll(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	r := thisObject(cx, this, "RegExp.prototype[Symbol.matchAll]")
	s := cx.ToStringValue(arg(args, 0))
	var flags storage.Value
	var matcher *storage.Object
	if p, ok := quiet.UnmodifiedPattern(cx.Quiet(), r); ok {
		flags = cx.RegExpFlagsString(p.Matcher.Flags())
		matcher = cloneRegExp(cx, p.Source, flags)
	} else {
		c := speciesConstructor(cx, r, cx.Intrinsic(storage.IntrinsicRegExp))
		flags = getFlags(cx, r)
		matcher = cx.Construct(c, []storage.Value{storage.ObjectValue(r), flags}, c)
	}
	lastIndex := getLastIndex(cx, r)
	setLastIndex(cx, matcher, lastIndex)
	fl := flags.Str()
	return storage.ObjectValue(cx.NewIteratorObject(cx.Intrinsic(storage.IntrinsicRegExpStringIteratorPrototype), &regexpStringIterator{
		r:           matcher,
		s:           s,
		global:      strings.Contains(string(fl), "g"),
		fullUnicode: strings.ContainsAny(string(fl), "uv"),
	}))
}

func regExpReplaceMethod(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.replace]")
	s := cx.ToStringValue(arg(args, 0))
	return regExpReplace(cx, rx, s, arg(args, 1))
}

func regExpSearch(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.search]")
	s := cx.ToStringValue(arg(args, 0))
	key := storage.NamedKey(cx.Key(storage.KeyLastIndex))
	prev := cx.Get(rx, key)
	if !storage.SameValue(prev, storage.Number(0)) {
		cx.Set(rx, key, storage.Number(0))
	}
	index := storage.Number(-1)
	caps, result := execMatch(cx, rx, s)
	if caps != nil {
		index = storage.Number(float64(caps[0]))
	}
	if cur := cx.Get(rx, key); !storage.SameValue(cur, prev) {
		cx.Set(rx, key, prev)
	}
	if result != nil {
		index = cx.Get(result, storage.NamedKey(cx.Key(storage.KeyIndex)))
	}
	return index
}

func regExpSplitMethod(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	rx := thisObject(cx, this, "RegExp.prototype[Symbol.split]")
	s := cx.ToStringValue(arg(args, 0))
	return regExpSplit(cx, rx, s, arg(args, 1))
}

func regExpEscapeMethod(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	v := arg(args, 0)
	if !v.IsString() {
		cx.ThrowTypeError("RegExp.escape requires a string")
	}
	return cx.NewString(regExpEscape(cx, v.Str()))
}

type regexpStringIterator struct {
	r           *storage.Object
	s           storage.Value
	global      bool
	fullUnicode bool
	done        bool
}

// match is one match that RegExp.prototype[Symbol.replace] uses.
type match struct {
	// res is the match array, when the original exec did not produce the match.
	res *storage.Object
	// matcher produced the capture indices. A later exec lookup can recompile the receiver.
	matcher *regexp.Regexp
	// capsStart and capsEnd are the range of the capture indices of a match of the original
	// exec in the buffer that holds them.
	capsStart, capsEnd int
}

func regExpReplace(cx *engine.ExecutionContext, rx *storage.Object, s storage.Value, replaceValue storage.Value) storage.Value {
	functional := replaceValue.IsCallable()
	var template wtf8.String
	if !functional {
		template = cx.ToString(replaceValue)
		if v, ok := replaceUnmodified(cx, rx, s, template); ok {
			return v
		}
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
				empty = cx.ToString(cx.Get(res, storage.IndexKey(0))) == ""
			}
		}
		if !global {
			break
		}
		if empty {
			i := getLastIndex(cx, rx)
			setLastIndex(cx, rx, quiet.AdvanceStringIndex(s, i, fullUnicode))
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
		var captures []storage.Value
		var named storage.Value
		if re := r.matcher; re != nil {
			caps := capsBuf[r.capsStart:r.capsEnd]
			position = caps[0]
			matched = s.Substring(caps[0], caps[1])
			n := len(caps)/2 - 1
			captures = make([]storage.Value, n)
			for i := range captures {
				captures[i] = quiet.Capture(cx.Quiet(), s, caps, i+1)
			}
			named = storage.Undefined
			if len(re.Names()) > 0 && (functional || strings.Contains(string(template), "$<")) {
				named = groupsObject(cx, re, func(i int) storage.Value { return captures[i-1] })
			}
		} else {
			res := r.res
			n := max(cx.LengthOfArrayLike(res)-1, 0)
			matched = cx.ToString(cx.Get(res, storage.IndexKey(0)))
			p := cx.ToIntegerOrInfinity(cx.Get(res, storage.NamedKey(cx.Key(storage.KeyIndex))))
			position = int(math.Max(math.Min(p, float64(length)), 0))
			// The length is not trusted to allocate: the values are read one by one, which may throw.
			captures = make([]storage.Value, 0, min(n, 1024))
			for i := range n {
				c := getAt(cx, res, i+1)
				if !c.IsUndefined() {
					c = cx.ToStringValue(c)
				}
				captures = append(captures, c)
			}
			named = cx.Get(res, storage.NamedKey(cx.Key(storage.KeyGroups)))
		}
		if !functional {
			if !named.IsUndefined() {
				named = storage.ObjectValue(cx.ToObject(named))
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
		callArgs := make([]storage.Value, 0, len(captures)+4)
		callArgs = append(callArgs, cx.NewString(matched))
		callArgs = append(callArgs, captures...)
		callArgs = append(callArgs, storage.Number(float64(position)), s)
		if !named.IsUndefined() {
			callArgs = append(callArgs, named)
		}
		replacement := cx.ToString(cx.Call(replaceValue.Object(), storage.Undefined, callArgs...))
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

// replaceUnmodified is regExpReplace with the string template for a RegExp object rx whose
// matches run no JavaScript. It reports false, having done nothing, when rx is not such an object,
// its lastIndex is not a number, or it is sticky but not global.
func replaceUnmodified(cx *engine.ExecutionContext, rx *storage.Object, s storage.Value, template wtf8.String) (storage.Value, bool) {
	re := cx.Quiet().UnmodifiedRegExpMatcher(rx)
	if re == nil {
		return storage.Undefined, false
	}
	// Each exec converts lastIndex, which can run JavaScript or throw unless it is a number.
	if v, _ := cx.Quiet().RegExpLastIndex(rx); !v.IsNumber() {
		return storage.Undefined, false
	}
	f := re.Flags()
	global := f&regexp.FlagGlobal != 0
	if !global && f&regexp.FlagSticky != 0 {
		return storage.Undefined, false
	}
	fullUnicode := f&(regexp.FlagUnicode|regexp.FlagUnicodeSets) != 0
	// Without the g flag, the match starts at 0 and lastIndex stays. With it, lastIndex changes
	// as each exec and each advance after an empty match change it, which no JavaScript can read
	// in between, but which agrees with the legacy static properties when the loop is stopped.
	if global {
		setLastIndex(cx, rx, 0)
	}
	length := s.StringLength()
	var b wtf8.Builder
	// next is the index after the last replaced match, or -1 once the result is too long.
	next := 0
	found := false
	for pos := 0; pos <= length; {
		caps := cx.Quiet().MatchRegExp(re, s, pos)
		if caps == nil {
			break
		}
		if !found {
			b.Grow(len(s.Str()))
			found = true
		}
		cx.Quiet().RecordRegExpMatch(rx, s, caps)
		// When the result gets too long, the remaining matches are found before the error is
		// thrown, as they can be observed.
		if next >= 0 {
			next = substituteMatch(cx, &b, re, s, caps, template, next)
		}
		if !global {
			break
		}
		pos = caps[1]
		if caps[0] == caps[1] {
			pos = int(quiet.AdvanceStringIndex(s, int64(pos), fullUnicode))
		}
		setLastIndex(cx, rx, int64(pos))
	}
	if global {
		setLastIndex(cx, rx, 0)
	}
	if !found {
		return s, true
	}
	if next < 0 {
		cx.ThrowRangeError("invalid string length")
	}
	if next < length {
		writeLimited(cx, &b, s.Substring(next, length))
	}
	return cx.NewString(b.String()), true
}

// substituteMatch writes to b the part of s from next up to a match of re with the capture
// indices caps, followed by its replacement by template. It returns the index after the match,
// or -1 when b would get longer than MaxStringLength.
func substituteMatch(cx *engine.ExecutionContext, b *wtf8.Builder, re *regexp.Regexp, s storage.Value, caps []int, template wtf8.String, next int) int {
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
	named := storage.Undefined
	if len(re.Names()) > 0 && strings.Contains(string(template), "$<") {
		named = groupsObject(cx, re, func(i int) storage.Value { return quiet.Capture(cx.Quiet(), s, caps, i) })
	}
	if !tryWriteSubstitution(cx, b, matched, s, position, substCaptures{str: s, caps: caps}, named, template) {
		return -1
	}
	return caps[1]
}

func regExpSplit(cx *engine.ExecutionContext, rx *storage.Object, s storage.Value, limit storage.Value) storage.Value {
	// For an unmodified rx, finding the constructor and constructing the splitter run no
	// JavaScript, so the splitter is only made when the algorithm can observe it.
	pattern, unmodified := quiet.UnmodifiedPattern(cx.Quiet(), rx)
	var c *storage.Object
	var splitter *storage.Object
	var unicodeMatching bool
	if unmodified {
		c = cx.Intrinsic(storage.IntrinsicRegExp)
		unicodeMatching = pattern.Matcher.Flags()&(regexp.FlagUnicode|regexp.FlagUnicodeSets) != 0
	} else {
		c = speciesConstructor(cx, rx, cx.Intrinsic(storage.IntrinsicRegExp))
		flags := getFlags(cx, rx).Str()
		unicodeMatching = strings.ContainsAny(string(flags), "uv")
		newFlags := flags
		if !strings.Contains(string(flags), "y") {
			newFlags += "y"
		}
		splitter = cx.Construct(c, []storage.Value{storage.ObjectValue(rx), cx.NewString(newFlags)}, c)
	}
	lim := uint32(math.MaxUint32)
	if !limit.IsUndefined() {
		lim = cx.ToUint32(limit)
	}
	if lim == 0 {
		return storage.ObjectValue(cx.NewArray(nil))
	}
	// When the splitter is a RegExp object that no code can see, and its exec is the original
	// one, searching for the next match is the same as trying every position. The matcher of rx
	// finds the same matches as that of the splitter, which differs only in the sticky flag.
	if splitter == nil {
		if v, ok := quiet.SplitUnmodified(cx.Quiet(), rx, pattern.Matcher, s, lim, unicodeMatching); ok {
			return v
		}
		// ToUint32 changed rx or %RegExp.prototype%.
		splitter = cloneRegExp(cx, pattern.Source, cx.RegExpFlagsString(pattern.Matcher.Flags()|regexp.FlagSticky))
	}
	// Reading exec from a splitter that is not unmodified can run JavaScript, so it is read
	// only where RegExpExec reads it.
	if c == cx.Intrinsic(storage.IntrinsicRegExp) {
		if v, ok := quiet.SplitUnmodified(cx.Quiet(), splitter, splitter.RegExpMatcher(), s, lim, unicodeMatching); ok {
			return v
		}
	}
	size := s.StringLength()
	if size == 0 {
		caps, result := execMatch(cx, splitter, s)
		if caps != nil || result != nil {
			return storage.ObjectValue(cx.NewArray(nil))
		}
		return storage.ObjectValue(cx.NewArray([]storage.Value{s}))
	}
	var out []storage.Value
	p, q := 0, 0
	for q < size {
		setLastIndex(cx, splitter, int64(q))
		z := regExpExec(cx, splitter, s)
		if z.IsNull() {
			q = int(quiet.AdvanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		end := int(min(getLastIndex(cx, splitter), int64(size)))
		if end == p {
			q = int(quiet.AdvanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		out = append(out, cx.Substring(s, p, q))
		if uint32(len(out)) == lim {
			return storage.ObjectValue(cx.NewArray(out))
		}
		p = end
		n := max(cx.LengthOfArrayLike(z.Object())-1, 0)
		for i := int64(1); i <= n; i++ {
			out = append(out, getAt(cx, z.Object(), i))
			if uint32(len(out)) == lim {
				return storage.ObjectValue(cx.NewArray(out))
			}
		}
		q = p
	}
	out = append(out, cx.Substring(s, p, size))
	return storage.ObjectValue(cx.NewArray(out))
}

// substCaptures is the captures of a match for GetSubstitution. They are vals, which holds
// strings and undefined, unless str is set, in which case they are the capture indices caps in str.
type substCaptures struct {
	vals []storage.Value
	str  storage.Value
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
	if b.Len()+len(s) > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	b.WriteString(s)
}

// tryWrite writes s to b, or reports false when b would get longer than MaxStringLength.
func tryWrite(b *wtf8.Builder, s wtf8.String) bool {
	if b.Len()+len(s) > storage.MaxStringLength {
		return false
	}
	b.WriteString(s)
	return true
}

// writeSubstitution implements GetSubstitution, writing the result to b. It throws a RangeError
// when b would get longer than MaxStringLength.
func writeSubstitution(cx *engine.ExecutionContext, b *wtf8.Builder, matched wtf8.String, str storage.Value, position int, captures substCaptures, named storage.Value, template wtf8.String) {
	if !tryWriteSubstitution(cx, b, matched, str, position, captures, named, template) {
		cx.ThrowRangeError("invalid string length")
	}
}

// tryWriteSubstitution is writeSubstitution, but reports false instead of throwing when b would
// get longer than MaxStringLength.
func tryWriteSubstitution(cx *engine.ExecutionContext, b *wtf8.Builder, matched wtf8.String, str storage.Value, position int, captures substCaptures, named storage.Value, template wtf8.String) bool {
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
	span func(s storage.Value, caps []int, group int) (start, end int)

	// group is the capturing group that span gets.
	group int
}

func lastMatchSpan(s storage.Value, caps []int, group int) (start, end int) {
	return caps[0], caps[1]
}

func lastParenSpan(s storage.Value, caps []int, group int) (start, end int) {
	return groupSpan(s, caps, len(caps)/2-1)
}

func leftContextSpan(s storage.Value, caps []int, group int) (start, end int) {
	return 0, caps[0]
}

func rightContextSpan(s storage.Value, caps []int, group int) (start, end int) {
	return caps[1], s.StringLength()
}

func groupSpan(s storage.Value, caps []int, group int) (start, end int) {
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

func legacyInputGetter(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	p := cx.ActiveNativeState().(*legacyStatic)
	if !this.IsObject() || this.Object() != cx.Intrinsic(storage.IntrinsicRegExp) {
		throwIncompatibleLegacyStatic(cx, p, "getter")
	}
	v := cx.LegacyRegExpInput()
	if v.IsUndefined() {
		throwUnavailableLegacyStatic(cx, p)
	}
	return v
}

func legacyInputSetter(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	p := cx.ActiveNativeState().(*legacyStatic)
	if !this.IsObject() || this.Object() != cx.Intrinsic(storage.IntrinsicRegExp) {
		throwIncompatibleLegacyStatic(cx, p, "setter")
	}
	cx.SetLegacyRegExpInput(cx.ToStringValue(arg(args, 0)))
	return storage.Undefined
}

// legacyMatchGetter is the getter of the properties other than input.
func legacyMatchGetter(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	p := cx.ActiveNativeState().(*legacyStatic)
	if !this.IsObject() || this.Object() != cx.Intrinsic(storage.IntrinsicRegExp) {
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
func initLegacyStatics(cx *engine.ExecutionContext, ctor *storage.Object) {
	cx.Quiet().UpdateLegacyRegExpStaticProperties(cx.NewString(""), nil)
	define := func(p legacyStatic) {
		name := wtf8.String(p.name)
		getter := nativeLegacyMatchGetter
		var set *storage.Object
		if p.span == nil {
			getter = nativeLegacyInputGetter
			set = cx.NewNativeWithState("set "+name, 1, nativeLegacyInputSetter, &p)
		}
		get := cx.NewNativeWithState("get "+name, 0, getter, &p)
		cx.DefineAccessor(ctor, cx.Intern(name), get, set, storage.FlagConfigurable)
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
