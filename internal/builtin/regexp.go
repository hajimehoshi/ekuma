// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// regexpEnv is the state that the RegExp built-ins of one VM share.
type regexpEnv struct {
	m regexp.Matcher

	// exec is the original RegExp.prototype.exec. When a RegExp object's exec is this function,
	// the algorithms that call it use the matcher directly, without creating match arrays.
	exec *engine.Object

	lastIndex, index, input, groups, indices, flags, source, execKey *engine.Object

	// flagStrings holds the value of the flags getter for each set of the 8 flags, once computed.
	flagStrings *[1 << 8]engine.Value

	legacy legacyStatics
}

// legacyStatics holds the values of the legacy static properties of RegExp.
type legacyStatics struct {
	// valid is cleared by a match of a RegExp object without the legacy features.
	valid bool
	input engine.Value
	// str and caps are the subject and the capture indices of the last match, if any.
	str  *engine.Object
	caps []int
}

// record updates the legacy static properties after a match of r.
func (l *legacyStatics) record(r *engine.Object, s *engine.Object, caps []int) {
	l.recordLegacy(r.RegExpLegacyFeaturesEnabled(), s, caps)
}

// recordLegacy updates the legacy static properties after a match of a RegExp object
// whose [[LegacyFeaturesEnabled]] is legacy.
func (l *legacyStatics) recordLegacy(legacy bool, s *engine.Object, caps []int) {
	if !legacy {
		l.valid = false
		return
	}
	l.valid = true
	l.input = engine.ObjectValue(s)
	l.str = s
	l.caps = append(l.caps[:0], caps...)
}

func (e *regexpEnv) key(k *engine.Object) engine.PropKey {
	return engine.NamedKey(k)
}

// flagsString returns the string of the flags f in the order of the flags getter.
func (e *regexpEnv) flagsString(vm *engine.VM, f regexp.Flags) engine.Value {
	if e.flagStrings == nil {
		e.flagStrings = new([1 << 8]engine.Value)
	}
	v := &e.flagStrings[f]
	if !v.IsString() {
		*v = vm.NewString(f.String())
	}
	return *v
}

// getFlags returns ToString(Get(r, "flags")).
func (e *regexpEnv) getFlags(vm *engine.VM, r *engine.Object) engine.Value {
	if vm.IsUnmodifiedRegExp(r) {
		return e.flagsString(vm, r.RegExpMatcher().Flags())
	}
	return vm.ToStringValue(vm.Get(r, e.key(e.flags)))
}

// getLastIndex returns ToLength(Get(r, "lastIndex")).
func (e *regexpEnv) getLastIndex(vm *engine.VM, r *engine.Object) int64 {
	v, ok := vm.RegExpLastIndex(r)
	if !ok {
		v = vm.Get(r, e.key(e.lastIndex))
	}
	if v.IsNumber() {
		n := v.Number()
		if n >= 0 && n <= engine.MaxStringLength {
			return int64(n)
		}
	}
	return numconv.ToLength(vm.ToNumber(v))
}

func (e *regexpEnv) setLastIndex(vm *engine.VM, r *engine.Object, n int64) {
	if !vm.SetRegExpLastIndex(r, engine.Number(float64(n))) {
		vm.Set(r, e.key(e.lastIndex), engine.Number(float64(n)))
	}
}

// advanceStringIndex implements AdvanceStringIndex.
func advanceStringIndex(s *engine.Object, index int64, unicode bool) int64 {
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

// builtinExecRaw implements RegExpBuiltinExec up to the creation of the match array.
// It returns the code unit indices of the captures, which are valid until the next match,
// or nil when there is no match.
func (e *regexpEnv) builtinExecRaw(vm *engine.VM, r *engine.Object, s *engine.Object) []int {
	// Converting lastIndex can run JavaScript that recompiles r.
	lastIndex := e.getLastIndex(vm, r)
	re := r.RegExpMatcher()
	f := re.Flags()
	global, sticky := f&regexp.FlagGlobal != 0, f&regexp.FlagSticky != 0
	if !global && !sticky {
		lastIndex = 0
	}
	if lastIndex > int64(s.StringLength()) {
		if global || sticky {
			e.setLastIndex(vm, r, 0)
		}
		return nil
	}
	caps := re.Match(&e.m, s.Str(), int(lastIndex), vm)
	if caps == nil {
		if global || sticky {
			e.setLastIndex(vm, r, 0)
		}
		return nil
	}
	if global || sticky {
		e.setLastIndex(vm, r, int64(caps[1]))
	}
	e.legacy.record(r, s, caps)
	return caps
}

// capture returns the capture i of caps in s, or undefined.
func capture(vm *engine.VM, s *engine.Object, caps []int, i int) engine.Value {
	a, b := caps[2*i], caps[2*i+1]
	if a < 0 {
		return engine.Undefined
	}
	if a == 0 && b == s.StringLength() {
		return engine.ObjectValue(s)
	}
	return vm.NewString(s.Substring(a, b))
}

// groupsObject returns the groups object of a match of re, or undefined when re has no named groups.
func groupsObject(vm *engine.VM, re *regexp.Regexp, vals func(i int) engine.Value) engine.Value {
	names := re.Names()
	if len(names) == 0 {
		return engine.Undefined
	}
	g := vm.NewObject(nil)
	matched := map[string]bool{}
	for i := 1; i <= re.GroupCount(); i++ {
		name := re.GroupName(i)
		if name == "" || matched[name] {
			continue
		}
		v := vals(i)
		if !v.IsUndefined() {
			matched[name] = true
		}
		vm.CreateDataPropertyOrThrow(g, engine.NamedKey(vm.Intern(name)), v)
	}
	return engine.ObjectValue(g)
}

// builtinExec implements RegExpBuiltinExec.
func (e *regexpEnv) builtinExec(vm *engine.VM, r *engine.Object, s *engine.Object) engine.Value {
	caps := e.builtinExecRaw(vm, r, s)
	if caps == nil {
		return engine.Null
	}
	re := r.RegExpMatcher()
	n := re.GroupCount()
	nprops := 3
	if re.Flags()&regexp.FlagHasIndices != 0 {
		nprops = 4
	}
	a := vm.NewArrayWithRoom(n+1, nprops)
	for i := 0; i <= n; i++ {
		a.SetElement(int64(i), capture(vm, s, caps, i))
	}
	var pairs []engine.Value
	if re.Flags()&regexp.FlagHasIndices != 0 {
		pairs = make([]engine.Value, n+1)
		for i := range pairs {
			if caps[2*i] >= 0 {
				pairs[i] = engine.ObjectValue(vm.NewArrayPair(
					engine.Number(float64(caps[2*i])),
					engine.Number(float64(caps[2*i+1])),
				))
			}
		}
	}
	index := caps[0]
	vm.CreateDataPropertyOrThrow(a, e.key(e.index), engine.Number(float64(index)))
	vm.CreateDataPropertyOrThrow(a, e.key(e.input), engine.ObjectValue(s))
	vm.CreateDataPropertyOrThrow(a, e.key(e.groups), groupsObject(vm, re, func(i int) engine.Value {
		v, _ := a.Element(int64(i))
		return v
	}))
	if pairs != nil {
		ia := vm.NewArray(pairs)
		vm.CreateDataPropertyOrThrow(ia, e.key(e.groups), groupsObject(vm, re, func(i int) engine.Value { return pairs[i] }))
		vm.CreateDataPropertyOrThrow(a, e.key(e.indices), engine.ObjectValue(ia))
	}
	return engine.ObjectValue(a)
}

// fastExec reports whether RegExpExec on r would call the original exec, and so whether
// builtinExecRaw can stand for it.
func (e *regexpEnv) fastExec(vm *engine.VM, r *engine.Object) (engine.Value, bool) {
	if vm.IsUnmodifiedRegExp(r) {
		return engine.ObjectValue(e.exec), true
	}
	exec := vm.Get(r, e.key(e.execKey))
	return exec, exec.IsObject() && exec.Object() == e.exec && r.RegExpMatcher() != nil
}

// cloneRegExp returns a new RegExp object with the pattern src and the flags flags. It does what
// Construct(%RegExp%, « r, flags ») does for a RegExp object r with the source src for which
// [engine.VM.IsUnmodifiedRegExp] holds.
func cloneRegExp(vm *engine.VM, src string, flags engine.Value) *engine.Object {
	return vm.RegExpInitialize(vm.RegExpAlloc(vm.Intrinsic(engine.IntrinsicRegExp)), vm.NewString(src), flags)
}

// regExpExecWith implements RegExpExec with the value of r.exec already read.
func (e *regexpEnv) regExpExecWith(vm *engine.VM, r *engine.Object, s *engine.Object, exec engine.Value) engine.Value {
	if exec.IsCallable() {
		res := vm.Call(exec.Object(), engine.ObjectValue(r), engine.ObjectValue(s))
		if !res.IsObject() && !res.IsNull() {
			vm.ThrowTypeError("the result of a RegExp exec method must be an object or null")
		}
		return res
	}
	if r.RegExpMatcher() == nil {
		vm.ThrowTypeError("RegExp exec method called on an incompatible receiver")
	}
	return e.builtinExec(vm, r, s)
}

// regExpExec implements RegExpExec.
func (e *regexpEnv) regExpExec(vm *engine.VM, r *engine.Object, s *engine.Object) engine.Value {
	exec := vm.Get(r, e.key(e.execKey))
	return e.regExpExecWith(vm, r, s, exec)
}

// isRegExp implements IsRegExp.
func isRegExp(vm *engine.VM, v engine.Value) bool {
	if !v.IsObject() {
		return false
	}
	if m := vm.GetV(v, vm.WellKnownSymbol(engine.SymbolMatch)); !m.IsUndefined() {
		return engine.ToBoolean(m)
	}
	return v.Object().IsRegExpObject()
}

// requireGlobalRegExp throws the TypeError of String.prototype[name] when o is a RegExp
// object, by IsRegExp, whose flags do not include g.
func requireGlobalRegExp(vm *engine.VM, o *engine.Object, name string) {
	var global bool
	if vm.IsUnmodifiedRegExp(o) {
		global = o.RegExpMatcher().Flags()&regexp.FlagGlobal != 0
	} else {
		if !isRegExp(vm, engine.ObjectValue(o)) {
			return
		}
		flags := vm.GetV(engine.ObjectValue(o), vm.Intern("flags"))
		vm.RequireObjectCoercible(flags, "String.prototype."+name)
		global = strings.Contains(vm.ToString(flags), "g")
	}
	if !global {
		vm.ThrowTypeError(name + " must be called with a global RegExp")
	}
}

// regExpCreate implements RegExpCreate.
func regExpCreate(vm *engine.VM, p, f engine.Value) *engine.Object {
	return vm.RegExpInitialize(vm.RegExpAlloc(vm.Intrinsic(engine.IntrinsicRegExp)), p, f)
}

// speciesConstructor implements SpeciesConstructor.
func speciesConstructor(vm *engine.VM, o *engine.Object, def *engine.Object) *engine.Object {
	c := vm.Get(o, engine.NamedKey(vm.Key(engine.KeyConstructor)))
	if c.IsUndefined() {
		return def
	}
	if !c.IsObject() {
		vm.ThrowTypeError("object.constructor is not an object")
	}
	s := vm.GetV(c, vm.WellKnownSymbol(engine.SymbolSpecies))
	if s.IsNullish() {
		return def
	}
	if s.IsObject() && vm.IsConstructor(s.Object()) {
		return s.Object()
	}
	vm.ThrowTypeError("object.constructor[Symbol.species] is not a constructor")
	return nil
}

// escapeRegExpPattern implements EscapeRegExpPattern.
func escapeRegExpPattern(src string) string {
	if src == "" {
		return "(?:)"
	}
	if !strings.ContainsAny(src, "/\n\r  ") {
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
				case strings.HasPrefix(rest, "\u2028"):
					b.WriteString("u2028")
					i += 2
				case strings.HasPrefix(rest, "\u2029"):
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
	return b.String()
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

func initRegExp(vm *engine.VM) {
	env := &regexpEnv{
		lastIndex: vm.Intern("lastIndex"),
		index:     vm.Intern("index"),
		input:     vm.Intern("input"),
		groups:    vm.Intern("groups"),
		indices:   vm.Intern("indices"),
		flags:     vm.Intern("flags"),
		source:    vm.Intern("source"),
		execKey:   vm.Intern("exec"),
	}
	proto := vm.Intrinsic(engine.IntrinsicRegExpPrototype)
	ctor := constructor(vm, "RegExp", 2, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		pattern, flags := arg(args, 0), arg(args, 1)
		patternIsRegExp := isRegExp(vm, pattern)
		nt := vm.GetNewTarget()
		if nt == nil {
			nt = vm.Intrinsic(engine.IntrinsicRegExp)
			if patternIsRegExp && flags.IsUndefined() {
				pc := vm.GetV(pattern, vm.Key(engine.KeyConstructor))
				if pc.IsObject() && pc.Object() == nt {
					return pattern
				}
			}
		}
		var p, f engine.Value
		switch {
		case pattern.IsObject() && pattern.Object().IsRegExpObject():
			src, fl := pattern.Object().RegExpOriginal()
			p = vm.NewString(src)
			if flags.IsUndefined() {
				f = vm.NewString(fl)
			} else {
				f = flags
			}
		case patternIsRegExp:
			p = vm.GetV(pattern, env.source)
			if flags.IsUndefined() {
				f = vm.GetV(pattern, env.flags)
			} else {
				f = flags
			}
		default:
			p, f = pattern, flags
		}
		o := vm.RegExpAlloc(nt)
		return engine.ObjectValue(vm.RegExpInitialize(o, p, f))
	})
	vm.SetIntrinsic(engine.IntrinsicRegExp, ctor)
	vm.DefineAccessor(ctor, vm.WellKnownSymbol(engine.SymbolSpecies), vm.NewNative("get [Symbol.species]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	}), nil, engine.FlagConfigurable)

	thisRegExp := func(vm *engine.VM, this engine.Value, what string) *engine.Object {
		if !this.IsObject() || this.Object().RegExpMatcher() == nil {
			vm.ThrowTypeError("RegExp.prototype." + what + " requires that 'this' be a RegExp object")
		}
		return this.Object()
	}

	// exec and test borrow the RegExp object of a literal they are called on. They run no script
	// code before the last use of it, except the conversion of an object argument.
	env.exec = method(vm, proto, "exec", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if arg(args, 0).IsObject() {
			this = vm.OwnRegExp(this)
		}
		r := thisRegExp(vm, this, "exec")
		s := vm.ToStringValue(arg(args, 0)).Object()
		return env.builtinExec(vm, r, s)
	})
	vm.LendRegExpLiteralReceivers(env.exec)
	vm.LendRegExpLiteralReceivers(method(vm, proto, "test", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if arg(args, 0).IsObject() {
			this = vm.OwnRegExp(this)
		}
		r := thisObject(vm, this, "RegExp.prototype.test")
		s := vm.ToStringValue(arg(args, 0)).Object()
		exec, ok := env.fastExec(vm, r)
		if ok {
			return engine.Bool(env.builtinExecRaw(vm, r, s) != nil)
		}
		return engine.Bool(!env.regExpExecWith(vm, r, s, exec).IsNull())
	}))
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := thisObject(vm, this, "RegExp.prototype.toString")
		src := vm.ToString(vm.Get(r, env.key(env.source)))
		fl := vm.ToString(vm.Get(r, env.key(env.flags)))
		return vm.NewString(wtf8.Concat(wtf8.Concat("/"+src, "/"), fl))
	})
	method(vm, proto, "compile", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := thisRegExp(vm, this, "compile")
		if !vm.IsRegExpOfCurrentRealm(o) {
			vm.ThrowTypeError("RegExp.prototype.compile cannot be used on a RegExp object of another realm")
		}
		if !o.RegExpLegacyFeaturesEnabled() {
			vm.ThrowTypeError("RegExp.prototype.compile cannot be used on a subclass instance")
		}
		pattern, flags := arg(args, 0), arg(args, 1)
		p, f := pattern, flags
		if pattern.IsObject() && pattern.Object().IsRegExpObject() {
			if !flags.IsUndefined() {
				vm.ThrowTypeError("cannot supply flags when constructing one RegExp from another")
			}
			src, fl := pattern.Object().RegExpOriginal()
			p, f = vm.NewString(src), vm.NewString(fl)
		}
		return engine.ObjectValue(vm.RegExpInitialize(o, p, f))
	})
	for _, fp := range flagProps {
		getter(vm, proto, vm.Intern(fp.name), fp.name, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			if !this.IsObject() {
				vm.ThrowTypeError("RegExp.prototype." + fp.name + " getter called on non-object")
			}
			re := this.Object().RegExpMatcher()
			if re == nil {
				if this.Object() == vm.Intrinsic(engine.IntrinsicRegExpPrototype) {
					return engine.Undefined
				}
				vm.ThrowTypeError("RegExp.prototype." + fp.name + " getter called on non-RegExp object")
			}
			return engine.Bool(re.Flags()&fp.flag != 0)
		})
	}
	flagKeys := make([]engine.PropKey, len(flagProps))
	for i, fp := range flagProps {
		flagKeys[i] = engine.NamedKey(vm.Intern(fp.name))
	}
	getter(vm, proto, env.flags, "flags", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := thisObject(vm, this, "RegExp.prototype.flags getter")
		if vm.IsUnmodifiedRegExp(r) {
			return env.flagsString(vm, r.RegExpMatcher().Flags())
		}
		var f regexp.Flags
		for i, fp := range flagProps {
			if engine.ToBoolean(vm.Get(r, flagKeys[i])) {
				f |= fp.flag
			}
		}
		return env.flagsString(vm, f)
	})
	getter(vm, proto, env.source, "source", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			vm.ThrowTypeError("RegExp.prototype.source getter called on non-object")
		}
		o := this.Object()
		if o.RegExpMatcher() == nil {
			if o == vm.Intrinsic(engine.IntrinsicRegExpPrototype) {
				return vm.NewString("(?:)")
			}
			vm.ThrowTypeError("RegExp.prototype.source getter called on non-RegExp object")
		}
		src, _ := o.RegExpOriginal()
		return vm.NewString(escapeRegExpPattern(src))
	})

	symMethod(vm, proto, vm.WellKnownSymbol(engine.SymbolMatch), "[Symbol.match]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(vm, this, "RegExp.prototype[Symbol.match]")
		s := vm.ToStringValue(arg(args, 0)).Object()
		flags := env.getFlags(vm, rx).Object().Str()
		if !strings.Contains(flags, "g") {
			return env.regExpExec(vm, rx, s)
		}
		fullUnicode := strings.ContainsAny(flags, "uv")
		env.setLastIndex(vm, rx, 0)
		var out []engine.Value
		for {
			var matchStr engine.Value
			if exec, ok := env.fastExec(vm, rx); ok {
				caps := env.builtinExecRaw(vm, rx, s)
				if caps == nil {
					break
				}
				matchStr = capture(vm, s, caps, 0)
			} else {
				res := env.regExpExecWith(vm, rx, s, exec)
				if res.IsNull() {
					break
				}
				matchStr = vm.ToStringValue(vm.Get(res.Object(), engine.IndexKey(0)))
			}
			out = append(out, matchStr)
			if matchStr.Object().Str() == "" {
				i := env.getLastIndex(vm, rx)
				env.setLastIndex(vm, rx, advanceStringIndex(s, i, fullUnicode))
			}
		}
		if len(out) == 0 {
			return engine.Null
		}
		return engine.ObjectValue(vm.NewArray(out))
	})

	iterProto := vm.NewObject(vm.Intrinsic(engine.IntrinsicIteratorPrototype))
	iteratorNext(vm, iterProto, func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		st, ok := engine.IteratorState(this).(*regexpStringIterator)
		if !ok {
			vm.ThrowTypeError("%RegExpStringIteratorPrototype%.next requires that 'this' be a RegExp String Iterator")
		}
		if st.done {
			return engine.Undefined, false
		}
		m := env.regExpExec(vm, st.r, st.s)
		if m.IsNull() {
			st.done = true
			return engine.Undefined, false
		}
		if !st.global {
			st.done = true
			return m, true
		}
		if vm.ToString(vm.Get(m.Object(), engine.IndexKey(0))) == "" {
			i := env.getLastIndex(vm, st.r)
			env.setLastIndex(vm, st.r, advanceStringIndex(st.s, i, st.fullUnicode))
		}
		return m, true
	})
	vm.Define(iterProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("RegExp String Iterator"), engine.FlagConfigurable)

	symMethod(vm, proto, vm.WellKnownSymbol(engine.SymbolMatchAll), "[Symbol.matchAll]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := thisObject(vm, this, "RegExp.prototype[Symbol.matchAll]")
		s := vm.ToStringValue(arg(args, 0)).Object()
		unmodified := vm.IsUnmodifiedRegExp(r)
		var c *engine.Object
		if !unmodified {
			c = speciesConstructor(vm, r, vm.Intrinsic(engine.IntrinsicRegExp))
		}
		flags := env.getFlags(vm, r)
		var matcher *engine.Object
		if unmodified {
			src, _ := r.RegExpOriginal()
			matcher = cloneRegExp(vm, src, flags)
		} else {
			matcher = vm.Construct(c, []engine.Value{engine.ObjectValue(r), flags}, c)
		}
		lastIndex := env.getLastIndex(vm, r)
		env.setLastIndex(vm, matcher, lastIndex)
		fl := flags.Object().Str()
		return engine.ObjectValue(vm.NewIteratorObject(iterProto, &regexpStringIterator{
			r:           matcher,
			s:           s,
			global:      strings.Contains(fl, "g"),
			fullUnicode: strings.ContainsAny(fl, "uv"),
		}))
	})

	symMethod(vm, proto, vm.WellKnownSymbol(engine.SymbolReplace), "[Symbol.replace]", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(vm, this, "RegExp.prototype[Symbol.replace]")
		s := vm.ToStringValue(arg(args, 0)).Object()
		return env.replace(vm, rx, s, arg(args, 1))
	})

	symMethod(vm, proto, vm.WellKnownSymbol(engine.SymbolSearch), "[Symbol.search]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(vm, this, "RegExp.prototype[Symbol.search]")
		s := vm.ToStringValue(arg(args, 0)).Object()
		key := env.key(env.lastIndex)
		prev := vm.Get(rx, key)
		if !engine.SameValue(prev, engine.Number(0)) {
			vm.Set(rx, key, engine.Number(0))
		}
		index := engine.Number(-1)
		var res engine.Value
		if exec, ok := env.fastExec(vm, rx); ok {
			if caps := env.builtinExecRaw(vm, rx, s); caps != nil {
				index = engine.Number(float64(caps[0]))
			}
		} else {
			res = env.regExpExecWith(vm, rx, s, exec)
		}
		if cur := vm.Get(rx, key); !engine.SameValue(cur, prev) {
			vm.Set(rx, key, prev)
		}
		if res.IsObject() {
			index = vm.Get(res.Object(), env.key(env.index))
		}
		return index
	})

	symMethod(vm, proto, vm.WellKnownSymbol(engine.SymbolSplit), "[Symbol.split]", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		rx := thisObject(vm, this, "RegExp.prototype[Symbol.split]")
		s := vm.ToStringValue(arg(args, 0)).Object()
		return env.split(vm, rx, s, arg(args, 1))
	})

	method(vm, ctor, "escape", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		if !v.IsString() {
			vm.ThrowTypeError("RegExp.escape requires a string")
		}
		return vm.NewString(regExpEscape(v.Object().Str()))
	})

	initLegacyStatics(vm, ctor, env)

	// The fast paths read these properties of an unmodified RegExp object without a lookup:
	// the flag accessors, exec, constructor and @@species for SpeciesConstructor, and @@match
	// for the IsRegExp of the RegExp constructor. The String methods that borrow RegExp objects
	// read @@match, @@replace, @@search and @@split without running script code.
	protoKeys := []*engine.Object{
		env.flags,
		env.execKey,
		vm.Key(engine.KeyConstructor),
		vm.WellKnownSymbol(engine.SymbolMatch),
		vm.WellKnownSymbol(engine.SymbolReplace),
		vm.WellKnownSymbol(engine.SymbolSearch),
		vm.WellKnownSymbol(engine.SymbolSplit),
	}
	for _, fp := range flagProps {
		protoKeys = append(protoKeys, vm.Intern(fp.name))
	}
	vm.RecordRegExpOriginals(protoKeys, []*engine.Object{vm.WellKnownSymbol(engine.SymbolSpecies)})
}

type regexpStringIterator struct {
	r           *engine.Object
	s           *engine.Object
	global      bool
	fullUnicode bool
	done        bool
}

// match is one match that RegExp.prototype[Symbol.replace] uses.
type match struct {
	// res is the match array, when the original exec did not produce the match.
	res *engine.Object
	// capsStart and capsEnd are the range of the capture indices of a match of the original
	// exec in the buffer that holds them.
	capsStart, capsEnd int
}

func (e *regexpEnv) replace(vm *engine.VM, rx *engine.Object, s *engine.Object, replaceValue engine.Value) engine.Value {
	functional := replaceValue.IsCallable()
	var template string
	if !functional {
		template = vm.ToString(replaceValue)
	}
	flags := e.getFlags(vm, rx).Object().Str()
	global := strings.Contains(flags, "g")
	fullUnicode := false
	if global {
		fullUnicode = strings.ContainsAny(flags, "uv")
		e.setLastIndex(vm, rx, 0)
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
		exec, fast := e.fastExec(vm, rx)
		var empty bool
		if fast {
			caps := e.builtinExecRaw(vm, rx, s)
			if caps == nil {
				break
			}
			found = true
			empty = caps[0] == caps[1]
			if !functional && len(results) == 0 {
				if b.Len() == 0 {
					b.Grow(len(s.Str()))
				}
				next = substituteMatch(vm, &b, rx.RegExpMatcher(), s, caps, template, next)
			} else {
				start := len(capsBuf)
				capsBuf = append(capsBuf, caps...)
				results = append(results, match{capsStart: start, capsEnd: len(capsBuf)})
			}
		} else {
			res := e.regExpExecWith(vm, rx, s, exec)
			if res.IsNull() {
				break
			}
			found = true
			results = append(results, match{res: res.Object()})
			if global {
				empty = vm.ToString(vm.Get(res.Object(), engine.IndexKey(0))) == ""
			}
		}
		if !global {
			break
		}
		if empty {
			i := e.getLastIndex(vm, rx)
			e.setLastIndex(vm, rx, advanceStringIndex(s, i, fullUnicode))
		}
	}
	if !found {
		return engine.ObjectValue(s)
	}
	var re *regexp.Regexp
	for _, r := range results {
		caps := capsBuf[r.capsStart:r.capsEnd]
		var matched string
		var position int
		var captures []engine.Value
		var named engine.Value
		if r.res == nil {
			if re == nil {
				re = rx.RegExpMatcher()
			}
			position = caps[0]
			matched = s.Substring(caps[0], caps[1])
			n := len(caps)/2 - 1
			captures = make([]engine.Value, n)
			for i := range captures {
				captures[i] = capture(vm, s, caps, i+1)
			}
			named = engine.Undefined
			if len(re.Names()) > 0 && (functional || strings.Contains(template, "$<")) {
				named = groupsObject(vm, re, func(i int) engine.Value { return captures[i-1] })
			}
		} else {
			res := r.res
			n := max(vm.LengthOfArrayLike(res)-1, 0)
			matched = vm.ToString(vm.Get(res, engine.IndexKey(0)))
			p := vm.ToIntegerOrInfinity(vm.Get(res, e.key(e.index)))
			position = int(math.Max(math.Min(p, float64(length)), 0))
			captures = make([]engine.Value, n)
			for i := range captures {
				c := getAt(vm, res, int64(i)+1)
				if !c.IsUndefined() {
					c = vm.ToStringValue(c)
				}
				captures[i] = c
			}
			named = vm.Get(res, e.key(e.groups))
		}
		if !functional {
			if !named.IsUndefined() {
				named = engine.ObjectValue(vm.ToObject(named))
			}
			if position < next {
				// The replacement is unused, but computing it can run JavaScript.
				var rb wtf8.Builder
				writeSubstitution(vm, &rb, matched, s, position, substCaptures{vals: captures}, named, template)
				continue
			}
			writeLimited(vm, &b, s.Substring(next, position))
			writeSubstitution(vm, &b, matched, s, position, substCaptures{vals: captures}, named, template)
			next = position + wtf8.Len(matched)
			continue
		}
		callArgs := make([]engine.Value, 0, len(captures)+4)
		callArgs = append(callArgs, vm.NewString(matched))
		callArgs = append(callArgs, captures...)
		callArgs = append(callArgs, engine.Number(float64(position)), engine.ObjectValue(s))
		if !named.IsUndefined() {
			callArgs = append(callArgs, named)
		}
		replacement := vm.ToString(vm.Call(replaceValue.Object(), engine.Undefined, callArgs...))
		if position >= next {
			writeLimited(vm, &b, s.Substring(next, position))
			writeLimited(vm, &b, replacement)
			next = position + wtf8.Len(matched)
		}
	}
	if next >= length {
		return vm.NewString(b.String())
	}
	writeLimited(vm, &b, s.Substring(next, length))
	return vm.NewString(b.String())
}

// substituteMatch writes to b the part of s from next up to a match of re with the capture
// indices caps, followed by its replacement by template. It returns the index after the match.
func substituteMatch(vm *engine.VM, b *wtf8.Builder, re *regexp.Regexp, s *engine.Object, caps []int, template string, next int) int {
	position := caps[0]
	if position < next {
		return next
	}
	writeLimited(vm, b, s.Substring(next, position))
	matched := s.Substring(caps[0], caps[1])
	if !strings.Contains(template, "$") {
		writeLimited(vm, b, template)
		return caps[1]
	}
	named := engine.Undefined
	if len(re.Names()) > 0 && strings.Contains(template, "$<") {
		named = groupsObject(vm, re, func(i int) engine.Value { return capture(vm, s, caps, i) })
	}
	writeSubstitution(vm, b, matched, s, position, substCaptures{str: s, caps: caps}, named, template)
	return caps[1]
}

func (e *regexpEnv) split(vm *engine.VM, rx *engine.Object, s *engine.Object, limit engine.Value) engine.Value {
	// For an unmodified rx, finding the constructor and constructing the splitter run no
	// JavaScript, so the splitter is only made when the algorithm can observe it.
	unmodified := vm.IsUnmodifiedRegExp(rx)
	var c *engine.Object
	if unmodified {
		c = vm.Intrinsic(engine.IntrinsicRegExp)
	} else {
		c = speciesConstructor(vm, rx, vm.Intrinsic(engine.IntrinsicRegExp))
	}
	flags := e.getFlags(vm, rx).Object().Str()
	unicodeMatching := strings.ContainsAny(flags, "uv")
	newFlags := flags
	if !strings.Contains(flags, "y") {
		newFlags += "y"
	}
	var splitter *engine.Object
	var src string
	var re *regexp.Regexp
	if unmodified {
		src, _ = rx.RegExpOriginal()
		re = rx.RegExpMatcher()
	} else {
		splitter = vm.Construct(c, []engine.Value{engine.ObjectValue(rx), vm.NewString(newFlags)}, c)
	}
	lim := uint32(math.MaxUint32)
	if !limit.IsUndefined() {
		lim = vm.ToUint32(limit)
	}
	var out []engine.Value
	if lim == 0 {
		return engine.ObjectValue(vm.NewArray(nil))
	}
	// When the splitter is a RegExp object that no code can see, and its exec is the original
	// one, searching for the next match is the same as trying every position. The matcher of rx
	// finds the same matches as that of the splitter, which differs only in the sticky flag.
	var fast bool
	if splitter == nil {
		// ToUint32 may have changed %RegExp.prototype%.
		fast = vm.IsUnmodifiedRegExp(rx)
		if !fast {
			splitter = cloneRegExp(vm, src, vm.NewString(newFlags))
		}
	}
	// Reading exec from a splitter that is not unmodified can run JavaScript, so it is read
	// only where RegExpExec reads it.
	var origExec bool
	if splitter != nil && vm.IsUnmodifiedRegExp(splitter) {
		origExec = true
		if c == vm.Intrinsic(engine.IntrinsicRegExp) {
			fast = true
			re = splitter.RegExpMatcher()
		}
	}
	size := s.StringLength()
	if size == 0 {
		switch {
		case fast:
			// The splitter is sticky and its lastIndex is 0, so it can only match at 0.
			if caps := re.Find(&e.m, s.Str(), 0, vm); caps != nil {
				e.legacy.recordLegacy(true, s, caps)
				return engine.ObjectValue(vm.NewArray(nil))
			}
		case origExec:
			if e.builtinExecRaw(vm, splitter, s) != nil {
				return engine.ObjectValue(vm.NewArray(nil))
			}
		default:
			if !e.regExpExec(vm, splitter, s).IsNull() {
				return engine.ObjectValue(vm.NewArray(nil))
			}
		}
		return engine.ObjectValue(vm.NewArray([]engine.Value{engine.ObjectValue(s)}))
	}
	p, q := 0, 0
	for q < size {
		var caps []int
		var z engine.Value
		var end int
		if fast {
			caps = re.Find(&e.m, s.Str(), q, vm)
			if caps == nil || caps[0] >= size {
				break
			}
			e.legacy.recordLegacy(true, s, caps)
			q = caps[0]
			end = caps[1]
		} else {
			e.setLastIndex(vm, splitter, int64(q))
			z = e.regExpExec(vm, splitter, s)
			if z.IsNull() {
				q = int(advanceStringIndex(s, int64(q), unicodeMatching))
				continue
			}
			end = int(min(e.getLastIndex(vm, splitter), int64(size)))
		}
		if end == p {
			q = int(advanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		out = append(out, vm.NewString(s.Substring(p, q)))
		if uint32(len(out)) == lim {
			return engine.ObjectValue(vm.NewArray(out))
		}
		p = end
		if fast {
			for i := 1; i < len(caps)/2; i++ {
				out = append(out, capture(vm, s, caps, i))
				if uint32(len(out)) == lim {
					return engine.ObjectValue(vm.NewArray(out))
				}
			}
		} else {
			n := max(vm.LengthOfArrayLike(z.Object())-1, 0)
			for i := int64(1); i <= n; i++ {
				out = append(out, getAt(vm, z.Object(), i))
				if uint32(len(out)) == lim {
					return engine.ObjectValue(vm.NewArray(out))
				}
			}
		}
		q = p
	}
	out = append(out, vm.NewString(s.Substring(p, size)))
	return engine.ObjectValue(vm.NewArray(out))
}

// substCaptures is the captures of a match for GetSubstitution. They are vals, which holds
// strings and undefined, unless str is set, in which case they are the capture indices caps in str.
type substCaptures struct {
	vals []engine.Value
	str  *engine.Object
	caps []int
}

// count returns the number of the captures.
func (c *substCaptures) count() int {
	if c.str == nil {
		return len(c.vals)
	}
	return len(c.caps)/2 - 1
}

// get returns the capture i, counted from 1, and whether it is not undefined.
func (c *substCaptures) get(i int) (string, bool) {
	if c.str == nil {
		v := c.vals[i-1]
		if v.IsUndefined() {
			return "", false
		}
		return v.Object().Str(), true
	}
	a, b := c.caps[2*i], c.caps[2*i+1]
	if a < 0 {
		return "", false
	}
	return c.str.Substring(a, b), true
}

// writeLimited writes s to b, and throws a RangeError instead when b would get longer than
// MaxStringLength.
func writeLimited(vm *engine.VM, b *wtf8.Builder, s string) {
	if b.Len()+len(s) > engine.MaxStringLength {
		vm.ThrowRangeError("invalid string length")
	}
	b.WriteString(s)
}

// writeSubstitution implements GetSubstitution, writing the result to b.
func writeSubstitution(vm *engine.VM, b *wtf8.Builder, matched string, str *engine.Object, position int, captures substCaptures, named engine.Value, template string) {
	length := str.StringLength()
	ncaps := captures.count()
	for i := 0; i < len(template); {
		c := template[i]
		if c != '$' || i+1 >= len(template) {
			j := strings.IndexByte(template[i+1:], '$')
			if j < 0 {
				writeLimited(vm, b, template[i:])
				break
			}
			writeLimited(vm, b, template[i:i+1+j])
			i += 1 + j
			continue
		}
		switch next := template[i+1]; {
		case next == '$':
			writeLimited(vm, b, "$")
			i += 2
		case next == '&':
			writeLimited(vm, b, matched)
			i += 2
		case next == '`':
			writeLimited(vm, b, str.Substring(0, position))
			i += 2
		case next == '\'':
			if tail := position + wtf8.Len(matched); tail < length {
				writeLimited(vm, b, str.Substring(tail, length))
			}
			i += 2
		case next >= '0' && next <= '9':
			digits := 1
			if i+2 < len(template) && template[i+2] >= '0' && template[i+2] <= '9' {
				digits = 2
			}
			index, _ := strconv.Atoi(template[i+1 : i+1+digits])
			if index > ncaps && digits == 2 {
				digits = 1
				index = int(next - '0')
			}
			ref := template[i : i+1+digits]
			if index >= 1 && index <= ncaps {
				if c, ok := captures.get(index); ok {
					writeLimited(vm, b, c)
				}
			} else {
				writeLimited(vm, b, ref)
			}
			i += len(ref)
		case next == '<':
			gt := strings.IndexByte(template[i:], '>')
			if gt < 0 || named.IsUndefined() {
				writeLimited(vm, b, "$<")
				i += 2
				continue
			}
			name := template[i+2 : i+gt]
			v := vm.Get(named.Object(), vm.ToPropertyKey(vm.NewString(name)))
			if !v.IsUndefined() {
				writeLimited(vm, b, vm.ToString(v))
			}
			i += gt + 1
		default:
			writeLimited(vm, b, "$")
			i++
		}
	}
}

// regExpEscape implements RegExp.escape.
func regExpEscape(s string) string {
	var b wtf8.Builder
	hex := func(prefix string, v rune, n int) {
		b.WriteString(prefix)
		h := strconv.FormatInt(int64(v), 16)
		for range n - len(h) {
			b.WriteString("0")
		}
		b.WriteString(h)
	}
	for i := 0; i < len(s); {
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
		case strings.ContainsRune(",-=<>#&!%:;@~'`\"", r) || isWhiteSpaceOrLineTerminator(r) || r >= 0xD800 && r <= 0xDFFF:
			if r <= 0xFF {
				hex("\\x", r, 2)
			} else {
				hex("\\u", r, 4)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isWhiteSpaceOrLineTerminator(r rune) bool {
	switch r {
	case '\t', '\v', '\f', ' ', 0xA0, 0xFEFF, '\n', '\r', 0x2028, 0x2029:
		return true
	}
	return r > 0x7F && unicode.Is(unicode.Zs, r)
}

// initLegacyStatics defines the legacy static properties of RegExp.
func initLegacyStatics(vm *engine.VM, ctor *engine.Object, env *regexpEnv) {
	l := &env.legacy
	l.valid = true
	l.input = vm.NewString("")
	str := func(a, b int) engine.Value {
		if a < 0 {
			return vm.NewString("")
		}
		return vm.NewString(l.str.Substring(a, b))
	}
	define := func(names []string, f func() engine.Value, settable bool) {
		for _, name := range names {
			check := func(vm *engine.VM, this engine.Value, what string) {
				if !this.IsObject() || this.Object() != ctor {
					vm.ThrowTypeError("RegExp." + name + " " + what + " called on an incompatible receiver")
				}
			}
			get := vm.NewNative("get "+name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				check(vm, this, "getter")
				if !l.valid {
					vm.ThrowTypeError("RegExp." + name + " is not available after a match of a subclass instance")
				}
				if l.str == nil && !settable {
					return vm.NewString("")
				}
				return f()
			})
			var set *engine.Object
			if settable {
				set = vm.NewNative("set "+name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
					check(vm, this, "setter")
					l.input = vm.ToStringValue(arg(args, 0))
					return engine.Undefined
				})
			}
			vm.DefineAccessor(ctor, vm.Intern(name), get, set, engine.FlagConfigurable)
		}
	}
	define([]string{"input", "$_"}, func() engine.Value {
		return l.input
	}, true)
	define([]string{"lastMatch", "$&"}, func() engine.Value {
		return str(l.caps[0], l.caps[1])
	}, false)
	define([]string{"lastParen", "$+"}, func() engine.Value {
		n := len(l.caps) / 2
		if n <= 1 {
			return vm.NewString("")
		}
		return str(l.caps[2*n-2], l.caps[2*n-1])
	}, false)
	define([]string{"leftContext", "$`"}, func() engine.Value {
		return str(0, l.caps[0])
	}, false)
	define([]string{"rightContext", "$'"}, func() engine.Value {
		return str(l.caps[1], l.str.StringLength())
	}, false)
	for i := 1; i <= 9; i++ {
		define([]string{"$" + strconv.Itoa(i)}, func() engine.Value {
			if 2*i+1 >= len(l.caps) {
				return vm.NewString("")
			}
			return str(l.caps[2*i], l.caps[2*i+1])
		}, false)
	}
}
