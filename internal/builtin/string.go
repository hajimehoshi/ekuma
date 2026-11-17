// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// thisString implements the receiver conversion of the String.prototype methods.
func thisString(vm *engine.VM, this engine.Value, what string) *engine.Object {
	if this.IsString() {
		return this.Object()
	}
	vm.RequireObjectCoercible(this, "String.prototype."+what)
	return vm.ToStringValue(this).Object()
}

func initString(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicStringPrototype)
	ctor := constructor(vm, "String", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := vm.NewString("")
		if len(args) > 0 {
			if vm.GetNewTarget() == nil && args[0].IsSymbol() {
				return vm.NewString(vm.DisplayString(args[0]))
			}
			s = vm.ToStringValue(args[0])
		}
		if nt := vm.GetNewTarget(); nt != nil {
			return engine.ObjectValue(vm.NewStringObject(s.Object(), vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicStringPrototype))))
		}
		return s
	})
	method(vm, ctor, "fromCharCode", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if len(args) == 1 {
			return vm.UnitString(uint16(vm.ToUint32(args[0])))
		}
		var b wtf8.Builder
		for _, a := range args {
			b.WriteUnit(uint16(vm.ToUint32(a)))
		}
		return vm.NewString(b.String())
	})
	method(vm, ctor, "fromCodePoint", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var b wtf8.Builder
		for _, a := range args {
			n := vm.ToNumber(a)
			if n != math.Trunc(n) || n < 0 || n > 0x10FFFF {
				vm.ThrowRangeError("invalid code point " + vm.ToString(a))
			}
			b.WriteRune(rune(n))
		}
		return vm.NewString(b.String())
	})
	method(vm, ctor, "raw", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cooked := vm.ToObject(arg(args, 0))
		raw := vm.ToObject(vm.GetV(engine.ObjectValue(cooked), vm.Key(engine.KeyRaw)))
		n := vm.LengthOfArrayLike(raw)
		var b wtf8.Builder
		for i := range n {
			b.WriteString(vm.ToString(getAt(vm, raw, i)))
			if i+1 < n && int(i)+1 < len(args) {
				b.WriteString(vm.ToString(args[i+1]))
			}
		}
		return vm.NewString(b.String())
	})

	thisValue := func(vm *engine.VM, this engine.Value, what string) engine.Value {
		s, ok := engine.ThisStringValue(this)
		if !ok {
			vm.ThrowTypeError("String.prototype." + what + " requires that 'this' be a String")
		}
		return s
	}
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return thisValue(vm, this, "toString")
	})
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return thisValue(vm, this, "valueOf")
	})
	method(vm, proto, "charAt", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "charAt")
		i := vm.ToIntegerOrInfinity(arg(args, 0))
		if i < 0 || i >= float64(s.StringLength()) {
			return vm.NewString("")
		}
		return vm.UnitString(s.UnitAt(int(i)))
	})
	method(vm, proto, "at", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "at")
		n := float64(s.StringLength())
		i := vm.ToIntegerOrInfinity(arg(args, 0))
		if i < 0 {
			i += n
		}
		if i < 0 || i >= n {
			return engine.Undefined
		}
		return vm.UnitString(s.UnitAt(int(i)))
	})
	method(vm, proto, "charCodeAt", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "charCodeAt")
		i := vm.ToIntegerOrInfinity(arg(args, 0))
		if i < 0 || i >= float64(s.StringLength()) {
			return engine.Number(math.NaN())
		}
		return engine.Number(float64(s.UnitAt(int(i))))
	})
	method(vm, proto, "codePointAt", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "codePointAt")
		i := vm.ToIntegerOrInfinity(arg(args, 0))
		n := s.StringLength()
		if i < 0 || i >= float64(n) {
			return engine.Undefined
		}
		u := s.UnitAt(int(i))
		if wtf8.IsHighSurrogate(rune(u)) && int(i)+1 < n {
			if lo := s.UnitAt(int(i) + 1); wtf8.IsLowSurrogate(rune(lo)) {
				return engine.Number(float64(0x10000 + (rune(u)-0xD800)<<10 + (rune(lo) - 0xDC00)))
			}
		}
		return engine.Number(float64(u))
	})
	method(vm, proto, "indexOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "indexOf")
		sub := vm.ToString(arg(args, 0))
		from := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
		return engine.Number(float64(s.UnitIndexOf(sub, from)))
	})
	method(vm, proto, "lastIndexOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "lastIndexOf")
		sub := vm.ToString(arg(args, 0))
		n := s.StringLength()
		from := n
		if p := vm.ToNumber(arg(args, 1)); p == p {
			from = engine.RelativeIndex(math.Max(numconv.ToIntegerOrInfinity(p), 0), n)
		}
		return engine.Number(float64(s.UnitLastIndexOf(sub, from)))
	})
	noRegExp := func(vm *engine.VM, v engine.Value, what string) {
		if isRegExp(vm, v) {
			vm.ThrowTypeError("first argument to String.prototype." + what + " must not be a regular expression")
		}
	}
	method(vm, proto, "includes", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "includes")
		noRegExp(vm, arg(args, 0), "includes")
		sub := vm.ToString(arg(args, 0))
		from := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
		return engine.Bool(s.UnitIndexOf(sub, from) >= 0)
	})
	method(vm, proto, "startsWith", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "startsWith")
		noRegExp(vm, arg(args, 0), "startsWith")
		sub := vm.ToString(arg(args, 0))
		n := s.StringLength()
		from := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(arg(args, 1)), 0), n)
		end := from + wtf8.Len(sub)
		return engine.Bool(end <= n && s.Substring(from, end) == sub)
	})
	method(vm, proto, "endsWith", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "endsWith")
		noRegExp(vm, arg(args, 0), "endsWith")
		sub := vm.ToString(arg(args, 0))
		n := s.StringLength()
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(e), 0), n)
		}
		start := end - wtf8.Len(sub)
		return engine.Bool(start >= 0 && s.Substring(start, end) == sub)
	})
	method(vm, proto, "slice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "slice")
		n := s.StringLength()
		start := engine.RelativeIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = engine.RelativeIndex(vm.ToIntegerOrInfinity(e), n)
		}
		if start >= end {
			return vm.NewString("")
		}
		return vm.NewString(s.Substring(start, end))
	})
	method(vm, proto, "substring", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "substring")
		n := s.StringLength()
		start := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(arg(args, 0)), 0), n)
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(e), 0), n)
		}
		if start > end {
			start, end = end, start
		}
		return vm.NewString(s.Substring(start, end))
	})
	method(vm, proto, "substr", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "substr")
		n := s.StringLength()
		start := engine.RelativeIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
		length := float64(n)
		if l := arg(args, 1); !l.IsUndefined() {
			length = vm.ToIntegerOrInfinity(l)
		}
		end := int(math.Min(float64(start)+math.Max(length, 0), float64(n)))
		if start >= end {
			return vm.NewString("")
		}
		return vm.NewString(s.Substring(start, end))
	})
	method(vm, proto, "concat", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "concat")
		var b wtf8.Builder
		b.WriteString(s.Str())
		for _, a := range args {
			b.WriteString(vm.ToString(a))
		}
		return vm.NewString(b.String())
	})
	method(vm, proto, "repeat", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "repeat")
		n := vm.ToIntegerOrInfinity(arg(args, 0))
		if n < 0 || math.IsInf(n, 0) {
			vm.ThrowRangeError("invalid count value: " + vm.ToString(arg(args, 0)))
		}
		if n == 0 || s.Str() == "" {
			return vm.NewString("")
		}
		if n*float64(len(s.Str())) > engine.MaxStringLength {
			vm.ThrowRangeError("invalid string length")
		}
		if wtf8.JoinsPair(s.Str(), s.Str()) {
			var b wtf8.Builder
			writeRepeated(vm, &b, s.Str(), int(n))
			return vm.NewString(b.String())
		}
		return vm.NewString(repeatString(vm, s.Str(), int(n)))
	})
	pad := func(name string, atStart bool) {
		method(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisString(vm, this, name)
			target := numconv.ToLength(vm.ToNumber(arg(args, 0)))
			n := int64(s.StringLength())
			fill := " "
			if f := arg(args, 1); !f.IsUndefined() {
				fill = vm.ToString(f)
			}
			if target <= n || fill == "" {
				return engine.ObjectValue(s)
			}
			if target > engine.MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
			fo := vm.NewString(fill).Object()
			fl := int64(fo.StringLength())
			need := target - n
			var b wtf8.Builder
			writeRepeated(vm, &b, fill, int(need/fl))
			b.WriteString(fo.Substring(0, int(need%fl)))
			if atStart {
				b.WriteString(s.Str())
				return vm.NewString(b.String())
			}
			return vm.NewString(wtf8.Concat(s.Str(), b.String()))
		})
	}
	pad("padStart", true)
	pad("padEnd", false)
	trim := func(name string, start, end bool) *engine.Object {
		return method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisString(vm, this, name)
			return vm.NewString(numconv.TrimWhiteSpace(s.Str(), start, end))
		})
	}
	trim("trim", true, true)
	trimStart := trim("trimStart", true, false)
	trimEnd := trim("trimEnd", false, true)
	vm.Define(proto, vm.Intern("trimLeft"), engine.ObjectValue(trimStart), engine.FlagsHidden)
	vm.Define(proto, vm.Intern("trimRight"), engine.ObjectValue(trimEnd), engine.FlagsHidden)
	// The HTML methods of Annex B.
	for _, e := range []struct {
		name string
		tag  string
		attr string
	}{
		{"anchor", "a", "name"}, {"big", "big", ""}, {"blink", "blink", ""}, {"bold", "b", ""},
		{"fixed", "tt", ""}, {"fontcolor", "font", "color"}, {"fontsize", "font", "size"},
		{"italics", "i", ""}, {"link", "a", "href"}, {"small", "small", ""}, {"strike", "strike", ""},
		{"sub", "sub", ""}, {"sup", "sup", ""},
	} {
		length := 0
		if e.attr != "" {
			length = 1
		}
		method(vm, proto, e.name, length, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			// CreateHTML.
			s := thisString(vm, this, e.name)
			var b wtf8.Builder
			b.WriteString("<" + e.tag)
			if e.attr != "" {
				v := vm.ToString(arg(args, 0))
				b.WriteString(" " + e.attr + "=\"")
				b.WriteString(strings.ReplaceAll(v, "\"", "&quot;"))
				b.WriteString("\"")
			}
			b.WriteString(">")
			b.WriteString(s.Str())
			b.WriteString("</" + e.tag + ">")
			if b.Len() > engine.MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
			return vm.NewString(b.String())
		})
	}
	// mapCase defines a case mapping method that applies ascii to ASCII-only strings and f to other strings.
	mapCase := func(name string, ascii func(string) string, f stringMapping) {
		asciiMap := contextFree(ascii)
		method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisString(vm, this, name)
			if s.IsASCII() {
				return vm.NewString(mapInChunks(vm, s.Str(), asciiMap, anyBoundary))
			}
			return vm.NewString(mapWellFormed(vm, s.Str(), f))
		})
	}
	upper := stringMapping{
		f: contextFree(func(s string) string {
			return cases.Upper(language.Und).String(s)
		}),
		boundary: lastBoundary(norm.NFD),
	}
	lower := stringMapping{
		f:        toLowerCase,
		boundary: runeBoundary,
	}
	mapCase("toUpperCase", strings.ToUpper, upper)
	mapCase("toLowerCase", strings.ToLower, lower)
	mapCase("toLocaleUpperCase", strings.ToUpper, upper)
	mapCase("toLocaleLowerCase", strings.ToLower, lower)
	nfc, nfd, nfkc, nfkd := normalization(norm.NFC), normalization(norm.NFD), normalization(norm.NFKC), normalization(norm.NFKD)
	method(vm, proto, "localeCompare", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "localeCompare")
		that := vm.ToString(arg(args, 0))
		if s.IsASCII() && wtf8.IsASCII(that) {
			// An ASCII string is in every normalization form.
			return engine.Number(float64(wtf8.Compare(s.Str(), that)))
		}
		return engine.Number(float64(wtf8.Compare(mapWellFormed(vm, s.Str(), nfc), mapWellFormed(vm, that, nfc))))
	})
	method(vm, proto, "normalize", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "normalize")
		form, quick := nfc, norm.NFC
		if f := arg(args, 0); !f.IsUndefined() {
			switch vm.ToString(f) {
			case "NFC":
			case "NFD":
				form, quick = nfd, norm.NFD
			case "NFKC":
				form, quick = nfkc, norm.NFKC
			case "NFKD":
				form, quick = nfkd, norm.NFKD
			default:
				vm.ThrowRangeError("the normalization form should be one of NFC, NFD, NFKC, NFKD")
			}
		}
		if s.IsASCII() {
			return engine.ObjectValue(s)
		}
		str := s.Str()
		if wtf8.IsWellFormed(str) && quick.QuickSpanString(str) == len(str) {
			return engine.ObjectValue(s)
		}
		return vm.NewString(mapWellFormed(vm, str, form))
	})
	method(vm, proto, "isWellFormed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(wtf8.IsWellFormed(thisString(vm, this, "isWellFormed").Str()))
	})
	method(vm, proto, "toWellFormed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(wtf8.ToValidUTF8(thisString(vm, this, "toWellFormed").Str()))
	})
	// split, replace, replaceAll, match and search borrow the RegExp object of a literal separator
	// or pattern. RegExp.prototype[@@split] and the others reach it only through IsUnmodifiedRegExp,
	// and run no script code before the last use of it, except the conversions of the other
	// arguments, which the conditions below rule out, and the replacer function, which runs last
	// and is not passed the object.
	vm.LendRegExpLiterals(method(vm, proto, "split", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.RequireObjectCoercible(this, "String.prototype.split")
		sepArg, limArg := arg(args, 0), arg(args, 1)
		if !this.IsString() || limArg.IsObject() {
			sepArg = vm.OwnRegExp(sepArg)
		}
		if sepArg.IsObject() {
			if m := vm.GetMethod(sepArg, vm.WellKnownSymbol(engine.SymbolSplit)); m != nil {
				return vm.Call(m, sepArg, this, limArg)
			}
		}
		s := vm.ToStringValue(this).Object()
		limit := uint32(math.MaxUint32)
		if !limArg.IsUndefined() {
			limit = vm.ToUint32(limArg)
		}
		sep := vm.ToString(sepArg)
		if limit == 0 {
			return engine.ObjectValue(vm.NewArray(nil))
		}
		if sepArg.IsUndefined() {
			return engine.ObjectValue(vm.NewArray([]engine.Value{engine.ObjectValue(s)}))
		}
		if sep == "" {
			n := int(min(uint64(s.StringLength()), uint64(limit)))
			out := make([]engine.Value, n)
			for i := 0; i < n; i += engine.PollInterval {
				if i > 0 {
					vm.Poll()
				}
				unitStrings(vm, out[i:min(i+engine.PollInterval, n)], s, i)
			}
			return engine.ObjectValue(vm.NewArray(out))
		}
		var out []engine.Value
		sepLen := wtf8.Len(sep)
		var start int
		var pacer engine.Pacer
		for uint32(len(out)) < limit {
			pacer.Step(vm)
			pos := s.UnitIndexOf(sep, start)
			if pos < 0 {
				out = append(out, vm.NewString(s.Substring(start, s.StringLength())))
				break
			}
			out = append(out, vm.NewString(s.Substring(start, pos)))
			start = pos + sepLen
		}
		return engine.ObjectValue(vm.NewArray(out))
	}))
	replace := func(name string, all bool) {
		vm.LendRegExpLiterals(method(vm, proto, name, 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			vm.RequireObjectCoercible(this, "String.prototype."+name)
			pat, rep := arg(args, 0), arg(args, 1)
			if !this.IsString() || rep.IsObject() && !rep.IsCallable() {
				pat = vm.OwnRegExp(pat)
			}
			if pat.IsObject() {
				if all {
					requireGlobalRegExp(vm, pat.Object(), "replaceAll")
				}
				if m := vm.GetMethod(pat, vm.WellKnownSymbol(engine.SymbolReplace)); m != nil {
					return vm.Call(m, pat, this, rep)
				}
			}
			s := vm.ToStringValue(this).Object()
			search := vm.ToString(pat)
			functional := rep.IsCallable()
			var repStr string
			if !functional {
				repStr = vm.ToString(rep)
			}
			n := s.StringLength()
			searchLen := wtf8.Len(search)
			advance := max(1, searchLen)
			var firstPosition [1]int
			var positions []int
			if !all {
				positions = firstPosition[:0]
			}
			var pacer engine.Pacer
			for pos := s.UnitIndexOf(search, 0); pos >= 0; {
				pacer.Step(vm)
				positions = append(positions, pos)
				if !all || pos+advance > n {
					break
				}
				pos = s.UnitIndexOf(search, pos+advance)
			}
			if len(positions) == 0 {
				return engine.ObjectValue(s)
			}
			var b wtf8.Builder
			if !functional {
				// The size of the result when repStr has no substitutions. A larger one throws below.
				if size := int64(len(s.Str())) + int64(len(positions))*int64(len(repStr)-len(search)); size <= engine.MaxStringLength {
					b.Grow(int(size))
				}
			}
			end := 0
			for _, p := range positions {
				pacer.Step(vm)
				writeLimited(vm, &b, s.Substring(end, p))
				if functional {
					writeLimited(vm, &b, vm.ToString(vm.Call(rep.Object(), engine.Undefined, vm.NewString(search), engine.Number(float64(p)), engine.ObjectValue(s))))
				} else {
					writeSubstitution(vm, &b, search, s, p, substCaptures{}, engine.Undefined, repStr)
				}
				end = p + searchLen
			}
			if end < n {
				writeLimited(vm, &b, s.Substring(end, n))
			}
			return vm.NewString(b.String())
		}))
	}
	replace("replace", false)
	replace("replaceAll", true)
	for _, e := range []struct {
		name string
		key  *engine.Object
	}{
		{"match", vm.WellKnownSymbol(engine.SymbolMatch)},
		{"matchAll", vm.WellKnownSymbol(engine.SymbolMatchAll)},
		{"search", vm.WellKnownSymbol(engine.SymbolSearch)},
	} {
		fn := method(vm, proto, e.name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			vm.RequireObjectCoercible(this, "String.prototype."+e.name)
			pat := arg(args, 0)
			if !this.IsString() {
				pat = vm.OwnRegExp(pat)
			}
			if pat.IsObject() {
				if e.name == "matchAll" {
					requireGlobalRegExp(vm, pat.Object(), "matchAll")
				}
				if m := vm.GetMethod(pat, e.key); m != nil {
					return vm.Call(m, pat, this)
				}
			}
			s := vm.ToStringValue(this)
			flags := engine.Undefined
			if e.name == "matchAll" {
				flags = vm.NewString("g")
			}
			rx := regExpCreate(vm, pat, flags)
			return vm.CallValue(vm.GetV(engine.ObjectValue(rx), e.key), engine.ObjectValue(rx), s)
		})
		if e.name != "matchAll" {
			vm.LendRegExpLiterals(fn)
		}
	}
	symMethod(vm, proto, vm.WellKnownSymbol(engine.SymbolIterator), "[Symbol.iterator]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "[Symbol.iterator]")
		return engine.ObjectValue(vm.NewIteratorObject(vm.Intrinsic(engine.IntrinsicStringIteratorPrototype), &stringIterator{str: s.Str()}))
	})
}

type stringIterator struct {
	str string
	pos int
}

// toLowerCase converts s[start:end] to lowercase with the root locale's full case mapping, in the
// context of the rest of s.
func toLowerCase(s string, start, end int) string {
	var b strings.Builder
	b.Grow(end - start)
	for k, r := range s[start:end] {
		i := start + k
		switch r {
		case '\u0130':
			// LATIN CAPITAL LETTER I WITH DOT ABOVE keeps the dot as a combining character.
			b.WriteString("i\u0307")
		case '\u03A3':
			if isFinalSigma(s, i) {
				b.WriteRune('\u03C2')
			} else {
				b.WriteRune('\u03C3')
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// isFinalSigma reports whether the character at byte offset i of s satisfies the Final_Sigma casing context.
func isFinalSigma(s string, i int) bool {
	// Before: a cased letter followed by zero or more case-ignorable characters.
	// A character that is both cased and case-ignorable counts as case-ignorable.
	var before bool
	for j := i; j > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
		if isCaseIgnorable(r) {
			continue
		}
		before = isCased(r)
		break
	}
	if !before {
		return false
	}
	// After: not zero or more case-ignorable characters followed by a cased letter.
	_, size := utf8.DecodeRuneInString(s[i:])
	for _, r := range s[i+size:] {
		if isCaseIgnorable(r) {
			continue
		}
		return !isCased(r)
	}
	return true
}

// isCased reports whether r has the Unicode Cased property.
func isCased(r rune) bool {
	return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// isCaseIgnorable reports whether r has the Unicode Case_Ignorable property.
func isCaseIgnorable(r rune) bool {
	switch r {
	// Word_Break values MidLetter, MidNumLet and Single_Quote.
	case '\'', '.', ':', '\u00B7', '\u0387', '\u055F', '\u05F4', '\u2018', '\u2019', '\u2024', '\u2027',
		'\uFE13', '\uFE52', '\uFE55', '\uFF07', '\uFF0E', '\uFF1A':
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// unitStrings sets the elements of out to the strings of the code units of s from the index start
// on.
func unitStrings(vm *engine.VM, out []engine.Value, s *engine.Object, start int) {
	for i := range out {
		out[i] = vm.UnitString(s.UnitAt(start + i))
	}
}

// stringMapping is a mapping of the well-formed strings.
type stringMapping struct {
	// f maps s[start:end], which begins and ends at boundaries, in the context of the rest of s.
	f func(s string, start, end int) string

	// boundary returns the offset of the last boundary in s other than its start, or a number
	// below 1 when there is none. The parts of a string between boundaries map independently.
	boundary func(s string) int
}

// contextFree returns f as the function of a stringMapping.
func contextFree(f func(string) string) func(s string, start, end int) string {
	return func(s string, start, end int) string {
		return f(s[start:end])
	}
}

// normalization returns the mapping to the normalization form.
func normalization(form norm.Form) stringMapping {
	return stringMapping{
		f:        contextFree(form.String),
		boundary: lastBoundary(form),
	}
}

// boundaryWindow is the number of bytes at the end of a string in which lastBoundary looks for a
// boundary.
const boundaryWindow = 256

// lastBoundary returns a boundary function for the normalization form: the parts of a string
// between its boundaries normalize independently.
func lastBoundary(form norm.Form) func(s string) int {
	return func(s string) int {
		w := max(0, len(s)-boundaryWindow)
		for w > 0 && s[w]&0xc0 == 0x80 {
			w--
		}
		i := form.LastBoundary([]byte(s[w:]))
		if i < 0 {
			return -1
		}
		return w + i
	}
}

// anyBoundary is the boundary function of a mapping of ASCII strings.
func anyBoundary(s string) int {
	return len(s)
}

// runeBoundary returns the offset of the start of the last character of s, which may be cut
// short.
func runeBoundary(s string) int {
	i := len(s) - 1
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// mapChunk is the number of bytes of a string that a mapping maps between two checks for an
// interruption.
const mapChunk = 1 << 16

// mapInChunks maps s with f, checking for an interruption between the parts of a long string,
// which end at boundaries that boundary finds.
func mapInChunks(vm *engine.VM, s string, f func(s string, start, end int) string, boundary func(s string) int) string {
	if len(s) <= mapChunk {
		return f(s, 0, len(s))
	}
	var b strings.Builder
	b.Grow(len(s))
	start := 0
	for len(s)-start > mapChunk {
		n := boundary(s[start : start+mapChunk])
		if n <= 0 {
			break
		}
		b.WriteString(f(s, start, start+n))
		start += n
		vm.Poll()
	}
	b.WriteString(f(s, start, len(s)))
	return b.String()
}

// mapWellFormed applies m to each maximal run of s without lone surrogates and keeps the lone
// surrogates as they are.
func mapWellFormed(vm *engine.VM, s string, m stringMapping) string {
	var b wtf8.Builder
	for len(s) > 0 {
		i := loneSurrogateIndex(s)
		b.WriteString(mapInChunks(vm, s[:i], m.f, m.boundary))
		s = s[i:]
		if len(s) > 0 {
			r, size := wtf8.DecodeRune(s)
			b.WriteRune(r)
			s = s[size:]
		}
	}
	return b.String()
}

// loneSurrogateIndex returns the offset of the first lone surrogate in s, or len(s).
func loneSurrogateIndex(s string) int {
	// A surrogate is ED A0..BF xx, and a well-formed character never starts with ED followed by
	// A0 or more.
	for i := 0; ; {
		j := strings.IndexByte(s[i:], 0xED)
		if j < 0 {
			return len(s)
		}
		i += j
		if i+1 < len(s) && s[i+1] >= 0xA0 {
			return i
		}
		i++
	}
}

// repeatChunk is the number of bytes of repeated copies of a string that writeRepeated writes
// between two checks for an interruption.
const repeatChunk = 1 << 20

// repeatString returns count copies of s, which does not join a surrogate pair with itself,
// checking for an interruption while it writes many bytes.
func repeatString(vm *engine.VM, s string, count int) string {
	total := len(s) * count
	if total <= repeatChunk {
		return strings.Repeat(s, count)
	}
	var b strings.Builder
	b.Grow(total)
	b.WriteString(s)
	// The copies written so far are copied in steps of whole copies.
	step := max(len(s), repeatChunk/len(s)*len(s))
	for b.Len() < total {
		vm.Poll()
		b.WriteString(b.String()[:min(b.Len(), total-b.Len(), step)])
	}
	return b.String()
}

// writeRepeated is b.WriteRepeated(s, count), checking for an interruption while it writes many
// bytes.
func writeRepeated(vm *engine.VM, b *wtf8.Builder, s string, count int) {
	per := max(1, repeatChunk/max(1, len(s)))
	if count > per {
		b.Grow(count * len(s))
	}
	for count > 0 {
		k := min(count, per)
		b.WriteRepeated(s, k)
		count -= k
		if count > 0 {
			vm.Poll()
		}
	}
}
