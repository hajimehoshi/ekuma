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
			for i := 0; i < int(n); i++ {
				b.WriteString(s.Str())
			}
			return vm.NewString(b.String())
		}
		return vm.NewString(strings.Repeat(s.Str(), int(n)))
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
			for i := int64(0); i < need/fl; i++ {
				b.WriteString(fill)
			}
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
	mapCase := func(name string, ascii, f func(string) string) {
		method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisString(vm, this, name)
			if s.IsASCII() {
				return vm.NewString(ascii(s.Str()))
			}
			return vm.NewString(mapWellFormed(s.Str(), f))
		})
	}
	upper := func(s string) string {
		return cases.Upper(language.Und).String(s)
	}
	lower := toLowerCase
	mapCase("toUpperCase", strings.ToUpper, upper)
	mapCase("toLowerCase", strings.ToLower, lower)
	mapCase("toLocaleUpperCase", strings.ToUpper, upper)
	mapCase("toLocaleLowerCase", strings.ToLower, lower)
	method(vm, proto, "localeCompare", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "localeCompare")
		that := vm.ToString(arg(args, 0))
		return engine.Number(float64(wtf8.Compare(mapWellFormed(s.Str(), norm.NFC.String), mapWellFormed(that, norm.NFC.String))))
	})
	method(vm, proto, "normalize", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "normalize")
		form := norm.NFC
		if f := arg(args, 0); !f.IsUndefined() {
			switch vm.ToString(f) {
			case "NFC":
			case "NFD":
				form = norm.NFD
			case "NFKC":
				form = norm.NFKC
			case "NFKD":
				form = norm.NFKD
			default:
				vm.ThrowRangeError("the normalization form should be one of NFC, NFD, NFKC, NFKD")
			}
		}
		if s.IsASCII() {
			return engine.ObjectValue(s)
		}
		return vm.NewString(mapWellFormed(s.Str(), form.String))
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
			for i := range out {
				out[i] = vm.UnitString(s.UnitAt(i))
			}
			return engine.ObjectValue(vm.NewArray(out))
		}
		var out []engine.Value
		sepLen := wtf8.Len(sep)
		var start int
		for uint32(len(out)) < limit {
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
			for pos := s.UnitIndexOf(search, 0); pos >= 0; {
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
			end := 0
			for _, p := range positions {
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

// toLowerCase converts s to lowercase with the root locale's full case mapping.
func toLowerCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
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

// mapWellFormed applies f to each maximal run of s without lone surrogates and keeps the lone surrogates as they are.
func mapWellFormed(s string, f func(string) string) string {
	var b wtf8.Builder
	for len(s) > 0 {
		var i int
		for i < len(s) {
			r, size := wtf8.DecodeRune(s[i:])
			if r >= 0xD800 && r <= 0xDFFF {
				break
			}
			i += size
		}
		b.WriteString(f(s[:i]))
		s = s[i:]
		if len(s) > 0 {
			r, size := wtf8.DecodeRune(s)
			b.WriteRune(r)
			s = s[size:]
		}
	}
	return b.String()
}
