// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/unorm"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// thisStringValue converts a String.prototype receiver to a string value.
func thisStringValue(cx *engine.ExecutionContext, this engine.Value, what string) engine.Value {
	if this.IsString() {
		return this
	}
	cx.RequireObjectCoercible(this, "String.prototype."+what)
	return cx.ToStringValue(this)
}

func requireThisString(cx *engine.ExecutionContext, this engine.Value, what string) engine.Value {
	if this.IsString() {
		return this
	}
	return requireThisStringObject(cx, this, what)
}

// requireThisStringObject is requireThisString for a receiver that is not a string.
func requireThisStringObject(cx *engine.ExecutionContext, this engine.Value, what string) engine.Value {
	s, ok := engine.ThisStringValue(this)
	if !ok {
		cx.ThrowTypeError("String.prototype." + what + " requires that 'this' be a String")
	}
	return s
}

// rejectRegExp throws a TypeError when the argument v of the method what is a regular expression.
func rejectRegExp(cx *engine.ExecutionContext, v engine.Value, what string) {
	if v.IsObject() {
		rejectRegExpObject(cx, v, what)
	}
}

func rejectRegExpObject(cx *engine.ExecutionContext, v engine.Value, what string) {
	if isRegExp(cx, v) {
		cx.ThrowTypeError("first argument to String.prototype." + what + " must not be a regular expression")
	}
}

func initString(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicStringPrototype)
	ctor := constructor(cx, "String", 1, engine.IntrinsicStringPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := cx.NewString("")
		if len(args) > 0 {
			if cx.GetNewTarget() == nil && args[0].IsSymbol() {
				sym, _ := engine.ThisSymbolValue(args[0])
				desc, _ := engine.SymbolDescription(sym)
				checkStringLength(cx, len(desc)+8)
				return cx.NewString("Symbol(" + desc + ")")
			}
			s = cx.ToStringValue(args[0])
		}
		if nt := cx.GetNewTarget(); nt != nil {
			return engine.ObjectValue(cx.NewStringObject(s.Object(), cx.GetPrototypeFromConstructor(nt, engine.IntrinsicStringPrototype)))
		}
		return s
	})
	// The kernel converts a number or a string, which needs no code to run and cannot throw.
	cx.SetMethodKernels(ctor, shortContract, nil, func(q engine.QuietContext, this, x engine.Value) (engine.Value, bool) {
		if x.IsString() {
			return x, true
		}
		if x.IsNumber() {
			return q.NumberToString(x.Number()), true
		}
		return engine.Undefined, false
	}, nil)
	fromCharCode := method(cx, ctor, "fromCharCode", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if len(args) == 1 {
			return cx.UnitString(uint16(cx.ToUint32(args[0])))
		}
		var buf [charactersOnStack]uint16
		units := slices.Grow(buf[:0], len(args))
		for _, a := range args {
			units = append(units, uint16(cx.ToUint32(a)))
		}
		s := cx.StringFromCodeUnits(units)
		checkStringLength(cx, s.StringByteLength())
		return s
	})
	cx.SetMethodKernels(fromCharCode, shortContract, nil, func(q engine.QuietContext, this, x engine.Value) (engine.Value, bool) {
		if !x.IsNumber() {
			return engine.Undefined, false
		}
		return q.UnitString(uint16(numconv.ToUint32(x.Number()))), true
	}, nil)
	method(cx, ctor, "fromCodePoint", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		var buf [charactersOnStack]rune
		points := slices.Grow(buf[:0], len(args))
		for _, a := range args {
			n := cx.ToNumber(a)
			if n != math.Trunc(n) || n < 0 || n > 0x10FFFF {
				cx.ThrowRangeError("invalid code point " + string(numconv.NumberToString(n)))
			}
			points = append(points, rune(n))
		}
		s := cx.CodePointsToString(points)
		checkStringLength(cx, s.StringByteLength())
		return s
	})
	method(cx, ctor, "raw", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cooked := cx.ToObject(arg(args, 0))
		raw := cx.ToObject(cx.GetV(engine.ObjectValue(cooked), cx.Key(engine.KeyRaw)))
		n := cx.LengthOfArrayLike(raw)
		var buf [stringPartsOnStack]engine.Value
		parts := buf[:0]
		var length int
		for i := range n {
			parts, length = appendStringPart(cx, parts, length, cx.ToStringValue(getAt(cx, raw, i)))
			if i+1 < n && int(i)+1 < len(args) {
				parts, length = appendStringPart(cx, parts, length, cx.ToStringValue(args[i+1]))
			}
		}
		return cx.ConcatStrings(parts)
	})

	method(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return requireThisString(cx, this, "toString")
	})
	method(cx, proto, "valueOf", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return requireThisString(cx, this, "valueOf")
	})
	// The kernels of the methods below are the methods for a string and arguments whose
	// conversions run no code.
	charAt := method(cx, proto, "charAt", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "charAt")
		return unitStringAt(cx.Quiet(), s, cx.ToIntegerOrInfinity(arg(args, 0)))
	})
	cx.SetMethodKernels(charAt, shortContract, nil, func(q engine.QuietContext, this, x engine.Value) (engine.Value, bool) {
		if !this.IsString() || !x.IsNumber() {
			return engine.Undefined, false
		}
		return unitStringAt(q, this, numconv.ToIntegerOrInfinity(x.Number())), true
	}, nil)
	method(cx, proto, "at", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "at")
		n := float64(s.StringLength())
		i := cx.ToIntegerOrInfinity(arg(args, 0))
		if i < 0 {
			i += n
		}
		if i < 0 || i >= n {
			return engine.Undefined
		}
		return cx.UnitString(s.UnitAt(int(i)))
	})
	charCodeAt := method(cx, proto, "charCodeAt", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "charCodeAt")
		return codeUnitAt(s, cx.ToIntegerOrInfinity(arg(args, 0)))
	})
	cx.SetMethodKernels(charCodeAt, shortContract, nil, func(q engine.QuietContext, this, x engine.Value) (engine.Value, bool) {
		if !this.IsString() || !x.IsNumber() {
			return engine.Undefined, false
		}
		return codeUnitAt(this, numconv.ToIntegerOrInfinity(x.Number())), true
	}, nil)
	method(cx, proto, "codePointAt", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "codePointAt")
		i := cx.ToIntegerOrInfinity(arg(args, 0))
		n := s.StringLength()
		if i < 0 || i >= float64(n) {
			return engine.Undefined
		}
		return engine.Number(float64(s.CodePointAt(int(i))))
	})
	indexOf := method(cx, proto, "indexOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "indexOf")
		sub := cx.ToStringValue(arg(args, 0))
		from := numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
		return engine.Number(float64(s.UnitIndexOf(sub, from, cx.Poll)))
	})
	cx.SetMethodKernels(indexOf, polledContract, nil, func(q engine.QuietContext, this, sub engine.Value) (engine.Value, bool) {
		if !this.IsString() || !sub.IsString() {
			return engine.Undefined, false
		}
		return engine.Number(float64(this.UnitIndexOf(sub, 0, q.Poll))), true
	}, nil)
	method(cx, proto, "lastIndexOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "lastIndexOf")
		sub := cx.ToStringValue(arg(args, 0))
		n := s.StringLength()
		from := n
		if p := cx.ToNumber(arg(args, 1)); p == p {
			from = numconv.RelativeIndex(math.Max(numconv.ToIntegerOrInfinity(p), 0), n)
		}
		return engine.Number(float64(s.UnitLastIndexOf(sub, from, cx.Poll)))
	})
	includes := method(cx, proto, "includes", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "includes")
		rejectRegExp(cx, arg(args, 0), "includes")
		sub := cx.ToStringValue(arg(args, 0))
		from := numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
		return engine.Bool(s.UnitIndexOf(sub, from, cx.Poll) >= 0)
	})
	cx.SetMethodKernels(includes, polledContract, nil, func(q engine.QuietContext, this, sub engine.Value) (engine.Value, bool) {
		if !this.IsString() || !sub.IsString() {
			return engine.Undefined, false
		}
		return engine.Bool(this.UnitIndexOf(sub, 0, q.Poll) >= 0), true
	}, nil)
	method(cx, proto, "startsWith", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "startsWith")
		rejectRegExp(cx, arg(args, 0), "startsWith")
		sub := cx.ToStringValue(arg(args, 0))
		n := s.StringLength()
		from := numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(arg(args, 1)), 0), n)
		end := from + sub.StringLength()
		return engine.Bool(end <= n && s.SubstringEquals(from, end, sub))
	})
	method(cx, proto, "endsWith", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "endsWith")
		rejectRegExp(cx, arg(args, 0), "endsWith")
		sub := cx.ToStringValue(arg(args, 0))
		n := s.StringLength()
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(e), 0), n)
		}
		start := end - sub.StringLength()
		return engine.Bool(start >= 0 && s.SubstringEquals(start, end, sub))
	})
	slice := method(cx, proto, "slice", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "slice")
		start, end := cx.ToIntegerOrInfinity(arg(args, 0)), math.Inf(1)
		if e := arg(args, 1); !e.IsUndefined() {
			end = cx.ToIntegerOrInfinity(e)
		}
		return sliceString(cx.Quiet(), s, start, end)
	})
	cx.SetMethodKernels(slice, shortContract, nil, func(q engine.QuietContext, this, start engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() {
			return engine.Undefined, false
		}
		return sliceString(q, this, start.Number(), math.Inf(1)), true
	}, func(q engine.QuietContext, this, start, end engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() || !end.IsNumber() {
			return engine.Undefined, false
		}
		return sliceString(q, this, start.Number(), end.Number()), true
	})
	substring := method(cx, proto, "substring", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "substring")
		start, end := cx.ToIntegerOrInfinity(arg(args, 0)), math.Inf(1)
		if e := arg(args, 1); !e.IsUndefined() {
			end = cx.ToIntegerOrInfinity(e)
		}
		return substringOf(cx.Quiet(), s, start, end)
	})
	cx.SetMethodKernels(substring, shortContract, nil, func(q engine.QuietContext, this, start engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() {
			return engine.Undefined, false
		}
		return substringOf(q, this, start.Number(), math.Inf(1)), true
	}, func(q engine.QuietContext, this, start, end engine.Value) (engine.Value, bool) {
		if !this.IsString() || !start.IsNumber() || !end.IsNumber() {
			return engine.Undefined, false
		}
		return substringOf(q, this, start.Number(), end.Number()), true
	})
	method(cx, proto, "substr", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "substr")
		n := s.StringLength()
		start := numconv.RelativeIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
		length := float64(n)
		if l := arg(args, 1); !l.IsUndefined() {
			length = cx.ToIntegerOrInfinity(l)
		}
		end := int(math.Min(float64(start)+math.Max(length, 0), float64(n)))
		if start >= end {
			return cx.NewString("")
		}
		return cx.Substring(s, start, end)
	})
	method(cx, proto, "concat", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cx.RequireObjectCoercible(this, "String.prototype.concat")
		s := cx.ToStringValue(this)
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
				writeStringLimited(cx, &b, a)
			}
			return cx.NewString(b.String())
		}
		var buf [stringPartsOnStack]engine.Value
		parts, length := append(buf[:0], s), s.StringByteLength()
		for _, a := range args {
			parts, length = appendStringPart(cx, parts, length, cx.ToStringValue(a))
		}
		return cx.ConcatStrings(parts)
	})
	method(cx, proto, "repeat", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "repeat")
		n := cx.ToIntegerOrInfinity(arg(args, 0))
		if n < 0 || math.IsInf(n, 0) {
			cx.ThrowRangeError("invalid count value: " + string(numconv.NumberToString(n)))
		}
		if n == 0 {
			return cx.NewString("")
		}
		byteLength := s.StringByteLength()
		if byteLength == 0 {
			return cx.NewString("")
		}
		if n*float64(byteLength) > engine.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
		if n == 1 {
			return s
		}
		if repeated, ok := cx.RepeatShort(s, int(n)); ok {
			return repeated
		}
		text := s.Str()
		if wtf8.JoinsPair(text, text) {
			var b wtf8.Buffer
			writeRepeated(cx, &b, text, int(n))
			return cx.NewString(b.String())
		}
		return cx.NewString(repeatString(cx, text, int(n)))
	})
	pad := func(name string, atStart bool) {
		method(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisStringValue(cx, this, name)
			target := numconv.ToLength(cx.ToNumber(arg(args, 0)))
			n := int64(s.StringLength())
			if target <= n {
				return s
			}
			fill := cx.NewString(" ")
			if f := arg(args, 1); !f.IsUndefined() {
				fill = cx.ToStringValue(f)
			}
			if fill.StringByteLength() == 0 {
				return s
			}
			if target > engine.MaxStringLength {
				cx.ThrowRangeError("invalid string length")
			}
			need := target - n
			if padded, ok := cx.PadShort(s, fill, int(target), int(need), atStart); ok {
				return padded
			}
			text := fill.Str()
			fo := fill.Object()
			fl := int64(fo.StringLength())
			var b wtf8.Buffer
			writeRepeated(cx, &b, text, int(need/fl))
			b.WriteString(fo.Substring(0, int(need%fl)))
			if atStart {
				b.WriteString(s.Str())
				return cx.NewString(b.String())
			}
			return cx.NewString(wtf8.Concat(s.Str(), b.String()))
		})
	}
	pad("padStart", true)
	pad("padEnd", false)
	trim := func(name string, start, end bool) *engine.Object {
		return method(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisStringValue(cx, this, name)
			return cx.TrimString(s, start, end)
		})
	}
	trim("trim", true, true)
	trimStart := trim("trimStart", true, false)
	trimEnd := trim("trimEnd", false, true)
	cx.Define(proto, cx.Intern("trimLeft"), engine.ObjectValue(trimStart), engine.FlagsHidden)
	cx.Define(proto, cx.Intern("trimRight"), engine.ObjectValue(trimEnd), engine.FlagsHidden)
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
		method(cx, proto, wtf8.String(e.name), length, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			// CreateHTML.
			s := thisStringValue(cx, this, e.name)
			var b wtf8.Buffer
			b.WriteString("<" + e.tag)
			if e.attr != "" {
				v := cx.ToString(arg(args, 0))
				b.WriteString(" " + e.attr + "=\"")
				b.WriteString(wtf8.String(strings.ReplaceAll(string(v), "\"", "&quot;")))
				b.WriteString("\"")
			}
			b.WriteString(">")
			s.WriteStringTo(&b)
			b.WriteString("</" + e.tag + ">")
			if b.Len() > engine.MaxStringLength {
				cx.ThrowRangeError("invalid string length")
			}
			return cx.NewString(b.String())
		})
	}
	// mapShortCase registers the non-locale methods with packed ASCII conversion.
	mapShortCase := func(name string, toUpper bool, ascii func(string) string, f stringMapping) {
		asciiMap := contextFree(ascii)
		method(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisStringValue(cx, this, name)
			if mapped, ok := cx.MapASCII(s, toUpper); ok {
				return mapped
			}
			o := s.Object()
			if o.IsASCII() {
				return cx.NewString(wtf8.String(mapInChunks(cx, string(o.Str()), asciiMap, anyBoundary)))
			}
			return cx.NewString(mapLimited(cx, o.Str(), f))
		})
	}
	// The uppercase mapping without a language maps each character on its own.
	upper := stringMapping{
		f:        contextFree(xstrings.ToUpperCase),
		boundary: runeBoundary,
	}
	lower := stringMapping{
		f:        xstrings.ToLowerCase,
		boundary: runeBoundary,
	}
	mapShortCase("toUpperCase", true, strings.ToUpper, upper)
	mapShortCase("toLowerCase", false, strings.ToLower, lower)
	mapShortCase("toLocaleUpperCase", true, strings.ToUpper, upper)
	mapShortCase("toLocaleLowerCase", false, strings.ToLower, lower)
	nfc, nfd, nfkc, nfkd := normalization(unorm.NFC), normalization(unorm.NFD), normalization(unorm.NFKC), normalization(unorm.NFKD)
	method(cx, proto, "localeCompare", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "localeCompare")
		that := cx.ToStringValue(arg(args, 0))
		if s.IsASCII() && that.IsASCII() {
			// An ASCII string is in every normalization form.
			return engine.Number(float64(engine.CompareStrings(s, that)))
		}
		return engine.Number(float64(wtf8.Compare(mapWellFormed(cx, s.Str(), nfc), mapWellFormed(cx, that.Str(), nfc))))
	})
	method(cx, proto, "normalize", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "normalize")
		form, quick := nfc, unorm.NFC
		if f := arg(args, 0); !f.IsUndefined() {
			switch cx.ToString(f) {
			case "NFC":
			case "NFD":
				form, quick = nfd, unorm.NFD
			case "NFKC":
				form, quick = nfkc, unorm.NFKC
			case "NFKD":
				form, quick = nfkd, unorm.NFKD
			default:
				cx.ThrowRangeError("the normalization form should be one of NFC, NFD, NFKC, NFKD")
			}
		}
		if s.IsASCII() {
			return s
		}
		str := s.Str()
		if wtf8.IsWellFormed(str) && unorm.QuickCheck(quick, string(str), cx.Poll) {
			return s
		}
		return cx.NewString(mapLimited(cx, str, form))
	})
	method(cx, proto, "isWellFormed", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisStringValue(cx, this, "isWellFormed").IsWellFormed())
	})
	method(cx, proto, "toWellFormed", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.ToWellFormed(thisStringValue(cx, this, "toWellFormed"))
	})
	// split, replace, replaceAll, match and search borrow the RegExp object of a literal separator
	// or pattern. RegExp.prototype[@@split] and the others reach it only through UnmodifiedRegExpMatcher,
	// and run no script code before the last use of it, except the conversions of the other
	// arguments, which the conditions below rule out, and the replacer function, which runs last
	// and is not passed the object.
	cx.LendRegExpLiterals(method(cx, proto, "split", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cx.RequireObjectCoercible(this, "String.prototype.split")
		sepArg, limArg := arg(args, 0), arg(args, 1)
		if !this.IsString() || limArg.IsObject() {
			sepArg = cx.OwnRegExp(sepArg)
		}
		if sepArg.IsObject() {
			if m := cx.GetMethod(sepArg, cx.WellKnownSymbol(engine.SymbolSplit)); m != nil {
				return cx.Call(m, sepArg, this, limArg)
			}
		}
		s := cx.ToStringValue(this)
		limit := uint32(math.MaxUint32)
		if !limArg.IsUndefined() {
			limit = cx.ToUint32(limArg)
		}
		sep := cx.ToStringValue(sepArg)
		if limit == 0 {
			return engine.ObjectValue(cx.NewArray(nil))
		}
		if sepArg.IsUndefined() {
			return engine.ObjectValue(cx.NewArray([]engine.Value{s}))
		}
		if sep.StringByteLength() == 0 {
			n := int(min(uint64(s.StringLength()), uint64(limit)))
			out := make([]engine.Value, n)
			for i := 0; i < n; i += engine.PollInterval {
				if i > 0 {
					cx.Poll()
				}
				unitStrings(cx, out[i:min(i+engine.PollInterval, n)], s, i)
			}
			return engine.ObjectValue(cx.NewArray(out))
		}
		var out []engine.Value
		sepLen := sep.StringLength()
		var start int
		var pacer engine.Pacer
		for uint32(len(out)) < limit {
			pos := s.UnitIndexOf(sep, start, cx.Poll)
			if pos < 0 {
				out = append(out, cx.Substring(s, start, s.StringLength()))
				break
			}
			out = append(out, cx.Substring(s, start, pos))
			// The search read what precedes the separator.
			pacer.Steps(cx, pos+sepLen-start)
			start = pos + sepLen
		}
		return engine.ObjectValue(cx.NewArray(out))
	}))
	replace := func(name string, all bool) {
		cx.LendRegExpLiterals(method(cx, proto, wtf8.String(name), 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			cx.RequireObjectCoercible(this, "String.prototype."+name)
			pat, rep := arg(args, 0), arg(args, 1)
			if !this.IsString() || rep.IsObject() && !rep.IsCallable() {
				pat = cx.OwnRegExp(pat)
			}
			if pat.IsObject() {
				if all {
					requireGlobalRegExp(cx, pat.Object(), "replaceAll")
				}
				if m := cx.GetMethod(pat, cx.WellKnownSymbol(engine.SymbolReplace)); m != nil {
					return cx.Call(m, pat, this, rep)
				}
			}
			s := cx.ToStringValue(this)
			search := cx.ToStringValue(pat)
			functional := rep.IsCallable()
			var repStr wtf8.String
			if !functional {
				repStr = cx.ToString(rep)
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
			from := 0
			for pos := s.UnitIndexOf(search, 0, cx.Poll); pos >= 0; {
				// The search read what precedes the match.
				pacer.Steps(cx, pos+advance-from)
				from = pos + advance
				positions = append(positions, pos)
				if !all || from > n {
					break
				}
				pos = s.UnitIndexOf(search, from, cx.Poll)
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
				pacer.Step(cx)
				writeLimited(cx, &b, s.Substring(end, p))
				if functional {
					writeLimited(cx, &b, cx.ToString(cx.Call(rep.Object(), engine.Undefined, search, engine.Number(float64(p)), s)))
				} else {
					writeSubstitution(cx, &b, search.Str(), s, p, substCaptures{}, engine.Undefined, repStr)
				}
				end = p + searchLen
			}
			if end < n {
				writeLimited(cx, &b, s.Substring(end, n))
			}
			return cx.NewString(b.String())
		}))
	}
	replace("replace", false)
	replace("replaceAll", true)
	for _, e := range []struct {
		name string
		key  *engine.Object
	}{
		{"match", cx.WellKnownSymbol(engine.SymbolMatch)},
		{"matchAll", cx.WellKnownSymbol(engine.SymbolMatchAll)},
		{"search", cx.WellKnownSymbol(engine.SymbolSearch)},
	} {
		fn := method(cx, proto, wtf8.String(e.name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			cx.RequireObjectCoercible(this, "String.prototype."+e.name)
			pat := arg(args, 0)
			if !this.IsString() {
				pat = cx.OwnRegExp(pat)
			}
			if pat.IsObject() {
				if e.name == "matchAll" {
					requireGlobalRegExp(cx, pat.Object(), "matchAll")
				}
				if m := cx.GetMethod(pat, e.key); m != nil {
					return cx.Call(m, pat, this)
				}
			}
			s := cx.ToStringValue(this)
			flags := engine.Undefined
			if e.name == "matchAll" {
				flags = cx.NewString("g")
			}
			rx := regExpCreate(cx, pat, flags)
			return cx.CallValue(cx.GetV(engine.ObjectValue(rx), e.key), engine.ObjectValue(rx), s)
		})
		if e.name != "matchAll" {
			cx.LendRegExpLiterals(fn)
		}
	}
	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolIterator), "[Symbol.iterator]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisStringValue(cx, this, "[Symbol.iterator]")
		return engine.ObjectValue(cx.NewIteratorObject(cx.Intrinsic(engine.IntrinsicStringIteratorPrototype), &stringIterator{str: s.Str()}))
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
func checkStringLength(cx *engine.ExecutionContext, n int) {
	if n > engine.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
}

// appendStringPart appends the string s to parts, whose strings have length bytes in all, and
// returns the new parts and length. It throws a RangeError instead when the length would pass
// MaxStringLength. The strings of a full parts are first joined into one, so parts never grows
// beyond its capacity.
func appendStringPart(cx *engine.ExecutionContext, parts []engine.Value, length int, s engine.Value) ([]engine.Value, int) {
	length += s.StringByteLength()
	checkStringLength(cx, length)
	if len(parts) == cap(parts) {
		parts = append(parts[:0], cx.ConcatStrings(parts))
	}
	return append(parts, s), length
}

// writeStringLimited writes ToString(v) to b, and throws a RangeError when b gets longer than
// MaxStringLength. A string v that would make b too long is not written.
func writeStringLimited(cx *engine.ExecutionContext, b *wtf8.Buffer, v engine.Value) {
	if v.IsString() {
		checkStringLength(cx, b.Len()+v.StringByteLength())
		v.WriteStringTo(b)
		return
	}
	writeString(cx, b, v)
	checkStringLength(cx, b.Len())
}

type stringIterator struct {
	str wtf8.String
	pos int
}

// unitStrings sets the elements of out to the strings of the code units of s from the index start
// on.
func unitStrings(cx *engine.ExecutionContext, out []engine.Value, s engine.Value, start int) {
	for i := range out {
		out[i] = cx.UnitString(s.UnitAt(start + i))
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
func normalization(form unorm.Form) stringMapping {
	return stringMapping{
		f: func(s string, start, end int, poll func()) string {
			return unorm.String(form, s[start:end], poll)
		},
		boundary: lastBoundary(form),
	}
}

// lastBoundary returns a boundary function for the normalization form: the parts of a string
// between its boundaries normalize independently.
func lastBoundary(form unorm.Form) func(s string) int {
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
func mapInChunks(cx *engine.ExecutionContext, s string, f func(s string, start, end int, poll func()) string, boundary func(s string) int) string {
	if len(s) <= mapChunk {
		return f(s, 0, len(s), cx.Poll)
	}
	var b strings.Builder
	b.Grow(len(s))
	start := 0
	for len(s)-start > mapChunk {
		n := nextBoundary(s[start:], boundary)
		b.WriteString(f(s, start, start+n, cx.Poll))
		start += n
		cx.Poll()
	}
	b.WriteString(f(s, start, len(s), cx.Poll))
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
func mapLimited(cx *engine.ExecutionContext, s wtf8.String, m stringMapping) wtf8.String {
	var n int
	f := m.f
	m.f = func(s string, start, end int, poll func()) string {
		mapped := f(s, start, end, poll)
		// The mapped parts make up the result, apart from the lone surrogates between them.
		n += len(mapped)
		checkStringLength(cx, n)
		return mapped
	}
	mapped := mapWellFormed(cx, s, m)
	checkStringLength(cx, len(mapped))
	return mapped
}

func mapWellFormed(cx *engine.ExecutionContext, s wtf8.String, m stringMapping) wtf8.String {
	return wtf8.MapWellFormed(s, func(s string) string {
		return mapInChunks(cx, s, m.f, m.boundary)
	})
}

// repeatChunk is the number of bytes of repeated copies of a string that writeRepeated writes
// between two checks for an interruption.
const repeatChunk = 1 << 20

// repeatString returns count copies of s, which does not join a surrogate pair with itself,
// checking for an interruption while it writes many bytes.
func repeatString(cx *engine.ExecutionContext, s wtf8.String, count int) wtf8.String {
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
		cx.Poll()
		b.WriteString(b.String()[:min(b.Len(), total-b.Len(), step)])
	}
	return wtf8.String(b.String())
}

// writeRepeated is b.WriteRepeated(s, count), checking for an interruption while it writes many
// bytes.
func writeRepeated(cx *engine.ExecutionContext, b *wtf8.Buffer, s wtf8.String, count int) {
	per := max(1, repeatChunk/max(1, len(s)))
	if count > per {
		b.Grow(count * len(s))
	}
	for count > 0 {
		k := min(count, per)
		b.WriteRepeated(s, k)
		count -= k
		if count > 0 {
			cx.Poll()
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
func unitStringAt(q engine.QuietContext, s engine.Value, i float64) engine.Value {
	if i < 0 || i >= float64(s.StringLength()) {
		return q.NewString("")
	}
	return q.UnitString(s.UnitAt(int(i)))
}

// sliceString returns the result of String.prototype.slice for the string s and the converted
// arguments start and end.
func sliceString(q engine.QuietContext, s engine.Value, start, end float64) engine.Value {
	n := s.StringLength()
	from, to := numconv.RelativeIndex(start, n), numconv.RelativeIndex(end, n)
	if from >= to {
		return q.NewString("")
	}
	return q.Substring(s, from, to)
}

// substringOf returns the result of String.prototype.substring for the string s and the converted
// arguments start and end.
func substringOf(q engine.QuietContext, s engine.Value, start, end float64) engine.Value {
	n := s.StringLength()
	from, to := numconv.RelativeIndex(max(start, 0), n), numconv.RelativeIndex(max(end, 0), n)
	if from > to {
		from, to = to, from
	}
	return q.Substring(s, from, to)
}
