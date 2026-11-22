// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/unorm"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// thisStringValue converts a String.prototype receiver to a string value.
func thisStringValue(vm *engine.VM, this engine.Value, what string) engine.Value {
	if this.IsString() {
		return this
	}
	vm.RequireObjectCoercible(this, "String.prototype."+what)
	return vm.ToStringValue(this)
}

func initString(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicStringPrototype)
	ctor := constructor(vm, "String", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := vm.NewString("")
		if len(args) > 0 {
			if vm.GetNewTarget() == nil && args[0].IsSymbol() {
				sym, _ := engine.ThisSymbolValue(args[0])
				desc, _ := engine.SymbolDescription(sym)
				checkStringLength(vm, len(desc)+8)
				return vm.NewString("Symbol(" + desc + ")")
			}
			s = vm.ToStringValue(args[0])
		}
		if nt := vm.GetNewTarget(); nt != nil {
			return engine.ObjectValue(vm.NewStringObject(s.Object(), vm.GetPrototypeFromConstructor(nt, engine.IntrinsicStringPrototype)))
		}
		return s
	})
	// The kernel converts a number or a string, which needs no code to run and cannot throw.
	vm.SetMethodKernels(ctor, nil, func(vm *engine.VM, this, x engine.Value) (engine.Value, bool) {
		if x.IsNumber() || x.IsString() {
			return vm.ToStringValue(x), true
		}
		return engine.Undefined, false
	}, nil)
	fromCharCode := method(vm, ctor, "fromCharCode", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if len(args) == 1 {
			return vm.UnitString(uint16(vm.ToUint32(args[0])))
		}
		var buf [charactersOnStack]uint16
		units := slices.Grow(buf[:0], len(args))
		for _, a := range args {
			units = append(units, uint16(vm.ToUint32(a)))
		}
		s := vm.StringFromCodeUnits(units)
		checkStringLength(vm, s.StringByteLength())
		return s
	})
	vm.SetMethodKernels(fromCharCode, nil, func(vm *engine.VM, this, x engine.Value) (engine.Value, bool) {
		if !x.IsNumber() {
			return engine.Undefined, false
		}
		return vm.UnitString(uint16(vm.ToUint32(x))), true
	}, nil)
	method(vm, ctor, "fromCodePoint", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var buf [charactersOnStack]rune
		points := slices.Grow(buf[:0], len(args))
		for _, a := range args {
			n := vm.ToNumber(a)
			if n != math.Trunc(n) || n < 0 || n > 0x10FFFF {
				vm.ThrowRangeError("invalid code point " + string(numconv.NumberToString(n)))
			}
			points = append(points, rune(n))
		}
		s := vm.CodePointsToString(points)
		checkStringLength(vm, s.StringByteLength())
		return s
	})
	method(vm, ctor, "raw", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cooked := vm.ToObject(arg(args, 0))
		raw := vm.ToObject(vm.GetV(engine.ObjectValue(cooked), vm.Key(engine.KeyRaw)))
		n := vm.LengthOfArrayLike(raw)
		var buf [stringPartsOnStack]engine.Value
		parts := buf[:0]
		var length int
		for i := range n {
			parts, length = appendStringPart(vm, parts, length, vm.ToStringValue(getAt(vm, raw, i)))
			if i+1 < n && int(i)+1 < len(args) {
				parts, length = appendStringPart(vm, parts, length, vm.ToStringValue(args[i+1]))
			}
		}
		return vm.ConcatStrings(parts)
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
	// The kernels of the methods below are the methods for a string and arguments whose
	// conversions run no code.
	charAt := method(vm, proto, "charAt", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "charAt")
		return unitStringAt(vm, s, vm.ToIntegerOrInfinity(arg(args, 0)))
	})
	vm.SetMethodKernels(charAt, nil, func(vm *engine.VM, this, x engine.Value) (engine.Value, bool) {
		if !this.IsString() || !x.IsNumber() {
			return engine.Undefined, false
		}
		return unitStringAt(vm, this, vm.ToIntegerOrInfinity(x)), true
	}, nil)
	method(vm, proto, "at", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "at")
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
	charCodeAt := method(vm, proto, "charCodeAt", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "charCodeAt")
		return codeUnitAt(s, vm.ToIntegerOrInfinity(arg(args, 0)))
	})
	vm.SetMethodKernels(charCodeAt, nil, func(vm *engine.VM, this, x engine.Value) (engine.Value, bool) {
		if !this.IsString() || !x.IsNumber() {
			return engine.Undefined, false
		}
		return codeUnitAt(this, vm.ToIntegerOrInfinity(x)), true
	}, nil)
	method(vm, proto, "codePointAt", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "codePointAt")
		i := vm.ToIntegerOrInfinity(arg(args, 0))
		n := s.StringLength()
		if i < 0 || i >= float64(n) {
			return engine.Undefined
		}
		return engine.Number(float64(s.CodePointAt(int(i))))
	})
	indexOf := method(vm, proto, "indexOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "indexOf")
		sub := vm.ToStringValue(arg(args, 0))
		from := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
		return engine.Number(float64(s.UnitIndexOf(sub, from, vm.Poll)))
	})
	vm.SetMethodKernels(indexOf, nil, func(vm *engine.VM, this, sub engine.Value) (engine.Value, bool) {
		if !this.IsString() || !sub.IsString() {
			return engine.Undefined, false
		}
		return engine.Number(float64(this.UnitIndexOf(sub, 0, vm.Poll))), true
	}, nil)
	method(vm, proto, "lastIndexOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "lastIndexOf")
		sub := vm.ToStringValue(arg(args, 0))
		n := s.StringLength()
		from := n
		if p := vm.ToNumber(arg(args, 1)); p == p {
			from = engine.RelativeIndex(math.Max(numconv.ToIntegerOrInfinity(p), 0), n)
		}
		return engine.Number(float64(s.UnitLastIndexOf(sub, from, vm.Poll)))
	})
	noRegExp := func(vm *engine.VM, v engine.Value, what string) {
		if isRegExp(vm, v) {
			vm.ThrowTypeError("first argument to String.prototype." + what + " must not be a regular expression")
		}
	}
	includes := method(vm, proto, "includes", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "includes")
		noRegExp(vm, arg(args, 0), "includes")
		sub := vm.ToStringValue(arg(args, 0))
		from := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
		return engine.Bool(s.UnitIndexOf(sub, from, vm.Poll) >= 0)
	})
	vm.SetMethodKernels(includes, nil, func(vm *engine.VM, this, sub engine.Value) (engine.Value, bool) {
		if !this.IsString() || !sub.IsString() {
			return engine.Undefined, false
		}
		return engine.Bool(this.UnitIndexOf(sub, 0, vm.Poll) >= 0), true
	}, nil)
	method(vm, proto, "startsWith", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "startsWith")
		noRegExp(vm, arg(args, 0), "startsWith")
		sub := vm.ToStringValue(arg(args, 0))
		n := s.StringLength()
		from := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(arg(args, 1)), 0), n)
		end := from + sub.StringLength()
		return engine.Bool(end <= n && s.SubstringEquals(from, end, sub))
	})
	method(vm, proto, "endsWith", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "endsWith")
		noRegExp(vm, arg(args, 0), "endsWith")
		sub := vm.ToStringValue(arg(args, 0))
		n := s.StringLength()
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(e), 0), n)
		}
		start := end - sub.StringLength()
		return engine.Bool(start >= 0 && s.SubstringEquals(start, end, sub))
	})
	slice := method(vm, proto, "slice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "slice")
		return sliceString(vm, s, arg(args, 0), arg(args, 1))
	})
	vm.SetMethodKernels(slice, nil, func(vm *engine.VM, this, start engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() {
			return engine.Undefined, false
		}
		return sliceString(vm, this, start, engine.Undefined), true
	}, func(vm *engine.VM, this, start, end engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() || !end.IsNumber() {
			return engine.Undefined, false
		}
		return sliceString(vm, this, start, end), true
	})
	substring := method(vm, proto, "substring", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "substring")
		return substringOf(vm, s, arg(args, 0), arg(args, 1))
	})
	vm.SetMethodKernels(substring, nil, func(vm *engine.VM, this, start engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() {
			return engine.Undefined, false
		}
		return substringOf(vm, this, start, engine.Undefined), true
	}, func(vm *engine.VM, this, start, end engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() || !end.IsNumber() {
			return engine.Undefined, false
		}
		return substringOf(vm, this, start, end), true
	})
	method(vm, proto, "substr", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "substr")
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
		return vm.Substring(s, start, end)
	})
	method(vm, proto, "concat", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.RequireObjectCoercible(this, "String.prototype.concat")
		s := vm.ToStringValue(this)
		byteLength := s.StringByteLength()
		reserve := byteLength <= engine.MaxStringLength
		for _, a := range args {
			if !a.IsString() {
				reserve = false
				break
			}
			n := a.StringByteLength()
			if n > engine.MaxStringLength-byteLength {
				reserve = false
				break
			}
			byteLength += n
		}
		// Two surrogate pairs can occupy twelve input bytes and fit after joining.
		if byteLength > 12 {
			var b wtf8.Buffer
			if reserve {
				b.Grow(byteLength)
			}
			s.WriteStringTo(&b)
			for _, a := range args {
				writeStringLimited(vm, &b, a)
			}
			return vm.NewString(b.String())
		}
		var buf [stringPartsOnStack]engine.Value
		parts, length := append(buf[:0], s), s.StringByteLength()
		for _, a := range args {
			parts, length = appendStringPart(vm, parts, length, vm.ToStringValue(a))
		}
		return vm.ConcatStrings(parts)
	})
	method(vm, proto, "repeat", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "repeat")
		n := vm.ToIntegerOrInfinity(arg(args, 0))
		if n < 0 || math.IsInf(n, 0) {
			vm.ThrowRangeError("invalid count value: " + string(numconv.NumberToString(n)))
		}
		if n == 0 {
			return vm.NewString("")
		}
		byteLength := s.StringByteLength()
		if byteLength == 0 {
			return vm.NewString("")
		}
		if n*float64(byteLength) > engine.MaxStringLength {
			vm.ThrowRangeError("invalid string length")
		}
		if n == 1 {
			return s
		}
		if repeated, ok := vm.RepeatShort(s, int(n)); ok {
			return repeated
		}
		text := s.Str()
		if wtf8.JoinsPair(text, text) {
			var b wtf8.Buffer
			writeRepeated(vm, &b, text, int(n))
			return vm.NewString(b.String())
		}
		return vm.NewString(repeatString(vm, text, int(n)))
	})
	pad := func(name string, atStart bool) {
		method(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisStringValue(vm, this, name)
			target := numconv.ToLength(vm.ToNumber(arg(args, 0)))
			n := int64(s.StringLength())
			if target <= n {
				return s
			}
			fill := vm.NewString(" ")
			if f := arg(args, 1); !f.IsUndefined() {
				fill = vm.ToStringValue(f)
			}
			if fill.StringByteLength() == 0 {
				return s
			}
			if target > engine.MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
			need := target - n
			if padded, ok := vm.PadShort(s, fill, int(target), int(need), atStart); ok {
				return padded
			}
			text := fill.Str()
			fo := fill.Object()
			fl := int64(fo.StringLength())
			var b wtf8.Buffer
			writeRepeated(vm, &b, text, int(need/fl))
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
		return method(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisStringValue(vm, this, name)
			return vm.TrimString(s, start, end)
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
		tag  wtf8.String
		attr wtf8.String
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
		method(vm, proto, wtf8.String(e.name), length, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			// CreateHTML.
			s := thisStringValue(vm, this, e.name)
			var b wtf8.Buffer
			b.WriteString("<" + e.tag)
			if e.attr != "" {
				v := vm.ToString(arg(args, 0))
				b.WriteString(" " + e.attr + "=\"")
				b.WriteString(wtf8.String(strings.ReplaceAll(string(v), "\"", "&quot;")))
				b.WriteString("\"")
			}
			b.WriteString(">")
			s.WriteStringTo(&b)
			b.WriteString("</" + e.tag + ">")
			if b.Len() > engine.MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
			return vm.NewString(b.String())
		})
	}
	// mapShortCase registers the non-locale methods with packed ASCII conversion.
	mapShortCase := func(name string, toUpper bool, ascii func(string) string, f stringMapping) {
		asciiMap := contextFree(ascii)
		method(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisStringValue(vm, this, name)
			if mapped, ok := vm.MapASCII(s, toUpper); ok {
				return mapped
			}
			o := s.Object()
			if o.IsASCII() {
				return vm.NewString(wtf8.String(mapInChunks(vm, string(o.Str()), asciiMap, anyBoundary)))
			}
			return vm.NewString(mapLimited(vm, o.Str(), f))
		})
	}
	// The uppercase mapping without a language maps each character on its own.
	upper := stringMapping{
		f: contextFree(func(s string) string {
			return cases.Upper(language.Und).String(s)
		}),
		boundary: runeBoundary,
	}
	lower := stringMapping{
		f: func(s string, start, end int, poll func()) string {
			return xstrings.ToLowerCase(s, start, end, poll)
		},
		boundary: runeBoundary,
	}
	mapShortCase("toUpperCase", true, strings.ToUpper, upper)
	mapShortCase("toLowerCase", false, strings.ToLower, lower)
	mapShortCase("toLocaleUpperCase", true, strings.ToUpper, upper)
	mapShortCase("toLocaleLowerCase", false, strings.ToLower, lower)
	nfc, nfd, nfkc, nfkd := normalization(norm.NFC), normalization(norm.NFD), normalization(norm.NFKC), normalization(norm.NFKD)
	method(vm, proto, "localeCompare", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "localeCompare")
		that := vm.ToStringValue(arg(args, 0))
		if s.IsASCII() && that.IsASCII() {
			// An ASCII string is in every normalization form.
			return engine.Number(float64(engine.CompareStrings(s, that)))
		}
		return engine.Number(float64(wtf8.Compare(mapWellFormed(vm, s.Str(), nfc), mapWellFormed(vm, that.Str(), nfc))))
	})
	method(vm, proto, "normalize", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(vm, this, "normalize")
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
			return s
		}
		str := s.Str()
		if wtf8.IsWellFormed(str) && quick.QuickSpanString(string(str)) == len(str) {
			return s
		}
		return vm.NewString(mapLimited(vm, str, form))
	})
	method(vm, proto, "isWellFormed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisStringValue(vm, this, "isWellFormed").IsWellFormed())
	})
	method(vm, proto, "toWellFormed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.ToWellFormed(thisStringValue(vm, this, "toWellFormed"))
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
		s := vm.ToStringValue(this)
		limit := uint32(math.MaxUint32)
		if !limArg.IsUndefined() {
			limit = vm.ToUint32(limArg)
		}
		sep := vm.ToStringValue(sepArg)
		if limit == 0 {
			return engine.ObjectValue(vm.NewArray(nil))
		}
		if sepArg.IsUndefined() {
			return engine.ObjectValue(vm.NewArray([]engine.Value{s}))
		}
		if sep.StringByteLength() == 0 {
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
		sepLen := sep.StringLength()
		var start int
		var pacer engine.Pacer
		for uint32(len(out)) < limit {
			pacer.Step(vm)
			pos := s.UnitIndexOf(sep, start, vm.Poll)
			if pos < 0 {
				out = append(out, vm.Substring(s, start, s.StringLength()))
				break
			}
			out = append(out, vm.Substring(s, start, pos))
			start = pos + sepLen
		}
		return engine.ObjectValue(vm.NewArray(out))
	}))
	replace := func(name string, all bool) {
		vm.LendRegExpLiterals(method(vm, proto, wtf8.String(name), 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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
			s := vm.ToStringValue(this)
			search := vm.ToStringValue(pat)
			functional := rep.IsCallable()
			var repStr wtf8.String
			if !functional {
				repStr = vm.ToString(rep)
			}
			n := s.StringLength()
			searchLen := search.StringLength()
			advance := max(1, searchLen)
			var firstPosition [1]int
			var positions []int
			if !all {
				positions = firstPosition[:0]
			}
			var pacer engine.Pacer
			for pos := s.UnitIndexOf(search, 0, vm.Poll); pos >= 0; {
				pacer.Step(vm)
				positions = append(positions, pos)
				if !all || pos+advance > n {
					break
				}
				pos = s.UnitIndexOf(search, pos+advance, vm.Poll)
			}
			if len(positions) == 0 {
				return s
			}
			var b wtf8.Builder
			if !functional {
				b.Grow(replacementCapacity(s.StringByteLength(), search.StringByteLength(), len(repStr), len(positions)))
			}
			end := 0
			for _, p := range positions {
				pacer.Step(vm)
				writeLimited(vm, &b, s.Substring(end, p))
				if functional {
					writeLimited(vm, &b, vm.ToString(vm.Call(rep.Object(), engine.Undefined, search, engine.Number(float64(p)), s)))
				} else {
					writeSubstitution(vm, &b, search.Str(), s, p, substCaptures{}, engine.Undefined, repStr)
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
		fn := method(vm, proto, wtf8.String(e.name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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
		s := thisStringValue(vm, this, "[Symbol.iterator]")
		return engine.ObjectValue(vm.NewIteratorObject(vm.Intrinsic(engine.IntrinsicStringIteratorPrototype), &stringIterator{str: s.Str()}))
	})
}

const (
	// charactersOnStack is the number of code units or code points a built-in collects without
	// allocating.
	charactersOnStack = 32

	// stringPartsOnStack is the number of strings a built-in collects for concatenation before
	// it joins them.
	stringPartsOnStack = 16
)

func replacementCapacity(inputBytes, searchBytes, replacementBytes, matches int) int {
	// A match can split a surrogate pair, so its standalone WTF-8 byte length
	// can overstate the bytes removed. Substitutions can also change the result size.
	size := int64(inputBytes) + int64(matches)*int64(replacementBytes-searchBytes)
	if size < 0 || size > engine.MaxStringLength {
		return 0
	}
	return int(size)
}

// checkStringLength throws a RangeError when n is more bytes than a string may have.
func checkStringLength(vm *engine.VM, n int) {
	if n > engine.MaxStringLength {
		vm.ThrowRangeError("invalid string length")
	}
}

// appendStringPart appends the string s to parts, whose strings have length bytes in all, and
// returns the new parts and length. It throws a RangeError instead when the length would pass
// MaxStringLength. The strings of a full parts are first joined into one, so parts never grows
// beyond its capacity.
func appendStringPart(vm *engine.VM, parts []engine.Value, length int, s engine.Value) ([]engine.Value, int) {
	length += s.StringByteLength()
	checkStringLength(vm, length)
	if len(parts) == cap(parts) {
		parts = append(parts[:0], vm.ConcatStrings(parts))
	}
	return append(parts, s), length
}

// writeStringLimited writes ToString(v) to b, and throws a RangeError when b gets longer than
// MaxStringLength. A string v that would make b too long is not written.
func writeStringLimited(vm *engine.VM, b *wtf8.Buffer, v engine.Value) {
	if v.IsString() {
		checkStringLength(vm, b.Len()+v.StringByteLength())
		v.WriteStringTo(b)
		return
	}
	writeString(vm, b, v)
	checkStringLength(vm, b.Len())
}

type stringIterator struct {
	str wtf8.String
	pos int
}

// unitStrings sets the elements of out to the strings of the code units of s from the index start
// on.
func unitStrings(vm *engine.VM, out []engine.Value, s engine.Value, start int) {
	for i := range out {
		out[i] = vm.UnitString(s.UnitAt(start + i))
	}
}

// stringMapping is a mapping of the well-formed strings.
type stringMapping struct {
	// f maps s[start:end], which begins and ends at boundaries, in the context of the rest of s.
	// poll is called now and then while f maps a long part.
	f func(s string, start, end int, poll func()) string

	// boundary returns the offset of the last boundary in s other than its start, or a number
	// below 1 when there is none. The parts of a string between boundaries map independently.
	boundary func(s string) int
}

// contextFree returns f as the function of a stringMapping.
func contextFree(f func(string) string) func(s string, start, end int, poll func()) string {
	return func(s string, start, end int, poll func()) string {
		return f(s[start:end])
	}
}

// normalization returns the mapping to the normalization form.
func normalization(form norm.Form) stringMapping {
	return stringMapping{
		f: func(s string, start, end int, poll func()) string {
			return unorm.String(form, s[start:end], poll)
		},
		boundary: lastBoundary(form),
	}
}

// lastBoundary returns a boundary function for the normalization form: the parts of a string
// between its boundaries normalize independently.
func lastBoundary(form norm.Form) func(s string) int {
	return func(s string) int {
		return unorm.LastBoundary(form, s)
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
func mapInChunks(vm *engine.VM, s string, f func(s string, start, end int, poll func()) string, boundary func(s string) int) string {
	if len(s) <= mapChunk {
		return f(s, 0, len(s), vm.Poll)
	}
	var b strings.Builder
	b.Grow(len(s))
	start := 0
	for len(s)-start > mapChunk {
		n := nextBoundary(s[start:], boundary)
		b.WriteString(f(s, start, start+n, vm.Poll))
		start += n
		vm.Poll()
	}
	b.WriteString(f(s, start, len(s), vm.Poll))
	return b.String()
}

// nextBoundary returns the offset of a boundary in s, which is longer than mapChunk, other than
// its start: one in the first mapChunk bytes, or else one at most about twice as far as the
// first one after them, or len(s).
func nextBoundary(s string, boundary func(s string) int) int {
	// A boundary function looks only near the end of the part that it is given.
	for n := mapChunk; n < len(s); n *= 2 {
		if i := boundary(s[:n]); i > 0 {
			return i
		}
	}
	return len(s)
}

// mapLimited is mapWellFormed, throwing a RangeError instead when the result would be longer than
// MaxStringLength.
func mapLimited(vm *engine.VM, s wtf8.String, m stringMapping) wtf8.String {
	var n int
	f := m.f
	m.f = func(s string, start, end int, poll func()) string {
		mapped := f(s, start, end, poll)
		// The mapped parts make up the result, apart from the lone surrogates between them.
		n += len(mapped)
		checkStringLength(vm, n)
		return mapped
	}
	mapped := mapWellFormed(vm, s, m)
	checkStringLength(vm, len(mapped))
	return mapped
}

func mapWellFormed(vm *engine.VM, s wtf8.String, m stringMapping) wtf8.String {
	return wtf8.MapWellFormed(s, func(s string) string {
		return mapInChunks(vm, s, m.f, m.boundary)
	})
}

// repeatChunk is the number of bytes of repeated copies of a string that writeRepeated writes
// between two checks for an interruption.
const repeatChunk = 1 << 20

// repeatString returns count copies of s, which does not join a surrogate pair with itself,
// checking for an interruption while it writes many bytes.
func repeatString(vm *engine.VM, s wtf8.String, count int) wtf8.String {
	total := len(s) * count
	if total <= repeatChunk {
		return wtf8.String(strings.Repeat(string(s), count))
	}
	var b strings.Builder
	b.Grow(total)
	b.WriteString(string(s))
	// The copies written so far are copied in steps of whole copies.
	step := max(len(s), repeatChunk/len(s)*len(s))
	for b.Len() < total {
		vm.Poll()
		b.WriteString(b.String()[:min(b.Len(), total-b.Len(), step)])
	}
	return wtf8.String(b.String())
}

// writeRepeated is b.WriteRepeated(s, count), checking for an interruption while it writes many
// bytes.
func writeRepeated(vm *engine.VM, b *wtf8.Buffer, s wtf8.String, count int) {
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

// codeUnitAt returns the code unit of the string s at the index i, or NaN when s has none there.
func codeUnitAt(s engine.Value, i float64) engine.Value {
	if i < 0 || i >= float64(s.StringLength()) {
		return engine.Number(math.NaN())
	}
	return engine.Number(float64(s.UnitAt(int(i))))
}

// unitStringAt returns the code unit of the string s at the index i as a string, or the empty
// string when s has none there.
func unitStringAt(vm *engine.VM, s engine.Value, i float64) engine.Value {
	if i < 0 || i >= float64(s.StringLength()) {
		return vm.NewString("")
	}
	return vm.UnitString(s.UnitAt(int(i)))
}

// sliceString implements String.prototype.slice for the string s and the arguments start and end.
func sliceString(vm *engine.VM, s, start, end engine.Value) engine.Value {
	n := s.StringLength()
	from := engine.RelativeIndex(vm.ToIntegerOrInfinity(start), n)
	to := n
	if !end.IsUndefined() {
		to = engine.RelativeIndex(vm.ToIntegerOrInfinity(end), n)
	}
	if from >= to {
		return vm.NewString("")
	}
	return vm.Substring(s, from, to)
}

// substringOf implements String.prototype.substring for the string s and the arguments start and
// end.
func substringOf(vm *engine.VM, s, start, end engine.Value) engine.Value {
	n := s.StringLength()
	from := engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(start), 0), n)
	to := n
	if !end.IsUndefined() {
		to = engine.RelativeIndex(math.Max(vm.ToIntegerOrInfinity(end), 0), n)
	}
	if from > to {
		from, to = to, from
	}
	return vm.Substring(s, from, to)
}
