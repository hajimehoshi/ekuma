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
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/unorm"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// thisStringValue converts a String.prototype receiver to a string value.
func thisStringValue(cx *engine.ExecutionContext, this storage.Value, what string) storage.Value {
	if this.IsString() {
		return this
	}
	cx.RequireObjectCoercible(this, "String.prototype."+what)
	return cx.ToStringValue(this)
}

func requireThisString(cx *engine.ExecutionContext, this storage.Value, what string) storage.Value {
	if this.IsString() {
		return this
	}
	return requireThisStringObject(cx, this, what)
}

// requireThisStringObject is requireThisString for a receiver that is not a string.
func requireThisStringObject(cx *engine.ExecutionContext, this storage.Value, what string) storage.Value {
	s, ok := storage.ThisStringValue(this)
	if !ok {
		cx.ThrowTypeError("String.prototype." + what + " requires that 'this' be a String")
	}
	return s
}

// rejectRegExp throws a TypeError when the argument v of the method what is a regular expression.
func rejectRegExp(cx *engine.ExecutionContext, v storage.Value, what string) {
	if v.IsObject() {
		rejectRegExpObject(cx, v, what)
	}
}

func rejectRegExpObject(cx *engine.ExecutionContext, v storage.Value, what string) {
	if isRegExp(cx, v) {
		cx.ThrowTypeError("first argument to String.prototype." + what + " must not be a regular expression")
	}
}

var (
	// The kernel converts a number or a string, which needs no code to run and cannot throw.
	nativeStringConstructor = engine.RegisterNativeWithKernels(stringConstructor, engine.Kernels{
		Method1:  quiet.StringConstructorKernel,
		Contract: shortContract,
	})
	nativeStringFromCharCode = engine.RegisterNativeWithKernels(stringFromCharCode, engine.Kernels{
		Method1:  quiet.StringFromCharCodeKernel,
		Contract: shortContract,
	})
	nativeStringFromCodePoint = engine.RegisterNative(stringFromCodePoint)
	nativeStringRaw           = engine.RegisterNative(stringRaw)
	nativeStringToString      = engine.RegisterNative(stringToString)
	nativeStringValueOf       = engine.RegisterNative(stringValueOf)
	// The kernels of the methods below are the methods for a string and arguments whose
	// conversions run no code.
	nativeStringCharAt = engine.RegisterNativeWithKernels(stringCharAt, engine.Kernels{
		Method1:  quiet.StringCharAtKernel,
		Contract: shortContract,
	})
	nativeStringAt         = engine.RegisterNative(stringAt)
	nativeStringCharCodeAt = engine.RegisterNativeWithKernels(stringCharCodeAt, engine.Kernels{
		Method1:  quiet.StringCharCodeAtKernel,
		Contract: shortContract,
	})
	nativeStringCodePointAt = engine.RegisterNative(stringCodePointAt)
	nativeStringIndexOf     = engine.RegisterNativeWithKernels(stringIndexOf, engine.Kernels{
		Method1:  quiet.StringIndexOfKernel,
		Contract: polledContract,
	})
	nativeStringLastIndexOf = engine.RegisterNative(stringLastIndexOf)
	nativeStringIncludes    = engine.RegisterNativeWithKernels(stringIncludes, engine.Kernels{
		Method1:  quiet.StringIncludesKernel,
		Contract: polledContract,
	})
	nativeStringStartsWith = engine.RegisterNative(stringStartsWith)
	nativeStringEndsWith   = engine.RegisterNative(stringEndsWith)
	nativeStringSlice      = engine.RegisterNativeWithKernels(stringSlice, engine.Kernels{
		Method1:  quiet.StringSliceKernel1,
		Method2:  quiet.StringSliceKernel2,
		Contract: shortContract,
	})
	nativeStringSubstring = engine.RegisterNativeWithKernels(stringSubstring, engine.Kernels{
		Method1:  quiet.StringSubstringKernel1,
		Method2:  quiet.StringSubstringKernel2,
		Contract: shortContract,
	})
	nativeStringSubstr         = engine.RegisterNative(stringSubstr)
	nativeStringConcat         = engine.RegisterNative(stringConcat)
	nativeStringRepeat         = engine.RegisterNative(stringRepeat)
	nativeStringLocaleCompare  = engine.RegisterNative(stringLocaleCompare)
	nativeStringNormalize      = engine.RegisterNative(stringNormalize)
	nativeStringIsWellFormed   = engine.RegisterNative(stringIsWellFormed)
	nativeStringToWellFormed   = engine.RegisterNative(stringToWellFormed)
	nativeStringSplit          = engine.RegisterNative(stringSplit)
	nativeStringIteratorMethod = engine.RegisterNative(stringIteratorMethod)
)

func initString(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(storage.IntrinsicStringPrototype)
	ctor := constructor(cx, "String", 1, storage.IntrinsicStringPrototype, nativeStringConstructor)
	method(cx, ctor, "fromCharCode", 1, nativeStringFromCharCode)
	method(cx, ctor, "fromCodePoint", 1, nativeStringFromCodePoint)
	method(cx, ctor, "raw", 1, nativeStringRaw)

	method(cx, proto, "toString", 0, nativeStringToString)
	method(cx, proto, "valueOf", 0, nativeStringValueOf)
	method(cx, proto, "charAt", 1, nativeStringCharAt)
	method(cx, proto, "at", 1, nativeStringAt)
	method(cx, proto, "charCodeAt", 1, nativeStringCharCodeAt)
	method(cx, proto, "codePointAt", 1, nativeStringCodePointAt)
	method(cx, proto, "indexOf", 1, nativeStringIndexOf)
	method(cx, proto, "lastIndexOf", 1, nativeStringLastIndexOf)
	method(cx, proto, "includes", 1, nativeStringIncludes)
	method(cx, proto, "startsWith", 1, nativeStringStartsWith)
	method(cx, proto, "endsWith", 1, nativeStringEndsWith)
	method(cx, proto, "slice", 2, nativeStringSlice)
	method(cx, proto, "substring", 2, nativeStringSubstring)
	method(cx, proto, "substr", 2, nativeStringSubstr)
	method(cx, proto, "concat", 1, nativeStringConcat)
	method(cx, proto, "repeat", 1, nativeStringRepeat)
	method(cx, proto, "padStart", 1, nativeStringPadStart)
	method(cx, proto, "padEnd", 1, nativeStringPadEnd)
	method(cx, proto, "trim", 0, nativeStringTrim)
	trimStart := method(cx, proto, "trimStart", 0, nativeStringTrimStart)
	trimEnd := method(cx, proto, "trimEnd", 0, nativeStringTrimEnd)
	cx.Define(proto, cx.Intern("trimLeft"), storage.ObjectValue(trimStart), storage.FlagsHidden)
	cx.Define(proto, cx.Intern("trimRight"), storage.ObjectValue(trimEnd), storage.FlagsHidden)
	// The HTML methods of Annex B.
	method(cx, proto, "anchor", 1, nativeStringAnchor)
	method(cx, proto, "big", 0, nativeStringBig)
	method(cx, proto, "blink", 0, nativeStringBlink)
	method(cx, proto, "bold", 0, nativeStringBold)
	method(cx, proto, "fixed", 0, nativeStringFixed)
	method(cx, proto, "fontcolor", 1, nativeStringFontcolor)
	method(cx, proto, "fontsize", 1, nativeStringFontsize)
	method(cx, proto, "italics", 0, nativeStringItalics)
	method(cx, proto, "link", 1, nativeStringLink)
	method(cx, proto, "small", 0, nativeStringSmall)
	method(cx, proto, "strike", 0, nativeStringStrike)
	method(cx, proto, "sub", 0, nativeStringSub)
	method(cx, proto, "sup", 0, nativeStringSup)

	method(cx, proto, "toUpperCase", 0, nativeStringToUpperCase)
	method(cx, proto, "toLowerCase", 0, nativeStringToLowerCase)
	method(cx, proto, "toLocaleUpperCase", 0, nativeStringToLocaleUpperCase)
	method(cx, proto, "toLocaleLowerCase", 0, nativeStringToLocaleLowerCase)
	method(cx, proto, "localeCompare", 1, nativeStringLocaleCompare)
	method(cx, proto, "normalize", 0, nativeStringNormalize)
	method(cx, proto, "isWellFormed", 0, nativeStringIsWellFormed)
	method(cx, proto, "toWellFormed", 0, nativeStringToWellFormed)
	// split, replace, replaceAll, match and search borrow the RegExp object of a literal separator
	// or pattern. RegExp.prototype[@@split] and the others reach it only through UnmodifiedRegExpMatcher,
	// and run no script code before the last use of it, except the conversions of the other
	// arguments, which the conditions below rule out, and the replacer function, which runs last
	// and is not passed the object.
	cx.LendRegExpLiterals(method(cx, proto, "split", 2, nativeStringSplit))
	cx.LendRegExpLiterals(method(cx, proto, "replace", 2, nativeStringReplace))
	cx.LendRegExpLiterals(method(cx, proto, "replaceAll", 2, nativeStringReplaceAll))
	cx.LendRegExpLiterals(method(cx, proto, "match", 1, nativeStringMatch))
	method(cx, proto, "matchAll", 1, nativeStringMatchAll)
	cx.LendRegExpLiterals(method(cx, proto, "search", 1, nativeStringSearch))
	symMethod(cx, proto, cx.WellKnownSymbol(storage.SymbolIterator), "[Symbol.iterator]", 0, nativeStringIteratorMethod)
}

func stringConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := cx.NewString("")
	if len(args) > 0 {
		if cx.GetNewTarget() == nil && args[0].IsSymbol() {
			sym, _ := storage.ThisSymbolValue(args[0])
			desc, _ := storage.SymbolDescription(sym)
			checkStringLength(cx, len(desc)+8)
			return cx.NewString("Symbol(" + desc + ")")
		}
		s = cx.ToStringValue(args[0])
	}
	if nt := cx.GetNewTarget(); nt != nil {
		return storage.ObjectValue(cx.NewStringObject(s.Object(), cx.GetPrototypeFromConstructor(nt, storage.IntrinsicStringPrototype)))
	}
	return s
}

func stringFromCharCode(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
}

func stringFromCodePoint(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
}

func stringRaw(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cooked := cx.ToObject(arg(args, 0))
	raw := cx.ToObject(cx.GetV(storage.ObjectValue(cooked), cx.Key(storage.KeyRaw)))
	n := cx.LengthOfArrayLike(raw)
	var buf [stringPartsOnStack]storage.Value
	parts := buf[:0]
	var length int
	for i := range n {
		parts, length = appendStringPart(cx, parts, length, cx.ToStringValue(getAt(cx, raw, i)))
		if i+1 < n && int(i)+1 < len(args) {
			parts, length = appendStringPart(cx, parts, length, cx.ToStringValue(args[i+1]))
		}
	}
	return cx.ConcatStrings(parts)
}

func stringToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return requireThisString(cx, this, "toString")
}

func stringValueOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return requireThisString(cx, this, "valueOf")
}

func stringCharAt(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "charAt")
	return quiet.UnitStringAt(cx.Quiet(), s, cx.ToIntegerOrInfinity(arg(args, 0)))
}

func stringAt(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "at")
	n := float64(s.StringLength())
	i := cx.ToIntegerOrInfinity(arg(args, 0))
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return storage.Undefined
	}
	return cx.UnitString(s.UnitAt(int(i)))
}

func stringCharCodeAt(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "charCodeAt")
	return quiet.CodeUnitAt(s, cx.ToIntegerOrInfinity(arg(args, 0)))
}

func stringCodePointAt(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "codePointAt")
	i := cx.ToIntegerOrInfinity(arg(args, 0))
	n := s.StringLength()
	if i < 0 || i >= float64(n) {
		return storage.Undefined
	}
	return storage.Number(float64(s.CodePointAt(int(i))))
}

func stringIndexOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "indexOf")
	sub := cx.ToStringValue(arg(args, 0))
	from := numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
	return storage.Number(float64(s.UnitIndexOf(sub, from, cx.Poll)))
}

func stringLastIndexOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "lastIndexOf")
	sub := cx.ToStringValue(arg(args, 0))
	n := s.StringLength()
	from := n
	if p := cx.ToNumber(arg(args, 1)); p == p {
		from = numconv.RelativeIndex(math.Max(numconv.ToIntegerOrInfinity(p), 0), n)
	}
	return storage.Number(float64(s.UnitLastIndexOf(sub, from, cx.Poll)))
}

func stringIncludes(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "includes")
	rejectRegExp(cx, arg(args, 0), "includes")
	sub := cx.ToStringValue(arg(args, 0))
	from := numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(arg(args, 1)), 0), s.StringLength())
	return storage.Bool(s.UnitIndexOf(sub, from, cx.Poll) >= 0)
}

func stringStartsWith(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "startsWith")
	rejectRegExp(cx, arg(args, 0), "startsWith")
	sub := cx.ToStringValue(arg(args, 0))
	n := s.StringLength()
	from := numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(arg(args, 1)), 0), n)
	end := from + sub.StringLength()
	return storage.Bool(end <= n && s.SubstringEquals(from, end, sub))
}

func stringEndsWith(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "endsWith")
	rejectRegExp(cx, arg(args, 0), "endsWith")
	sub := cx.ToStringValue(arg(args, 0))
	n := s.StringLength()
	end := n
	if e := arg(args, 1); !e.IsUndefined() {
		end = numconv.RelativeIndex(math.Max(cx.ToIntegerOrInfinity(e), 0), n)
	}
	start := end - sub.StringLength()
	return storage.Bool(start >= 0 && s.SubstringEquals(start, end, sub))
}

func stringSlice(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "slice")
	start, end := cx.ToIntegerOrInfinity(arg(args, 0)), math.Inf(1)
	if e := arg(args, 1); !e.IsUndefined() {
		end = cx.ToIntegerOrInfinity(e)
	}
	return quiet.SliceString(cx.Quiet(), s, start, end)
}

func stringSubstring(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "substring")
	start, end := cx.ToIntegerOrInfinity(arg(args, 0)), math.Inf(1)
	if e := arg(args, 1); !e.IsUndefined() {
		end = cx.ToIntegerOrInfinity(e)
	}
	return quiet.SubstringOf(cx.Quiet(), s, start, end)
}

func stringSubstr(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
}

func stringConcat(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cx.RequireObjectCoercible(this, "String.prototype.concat")
	s := cx.ToStringValue(this)
	byteLength := s.StringByteLength()
	reserve := byteLength <= storage.MaxStringLength
	for _, a := range args {
		if !a.IsString() {
			reserve = false
			break
		}
		n := a.StringByteLength()
		if n > storage.MaxStringLength-byteLength {
			reserve = false
			break
		}
		byteLength += n
	}
	// Two surrogate pairs can occupy twelve input bytes and fit after joining.
	if byteLength > 12 {
		if reserve && len(args) > 0 {
			if v, ok := cx.Quiet().ConcatASCIIStrings(s, args, byteLength); ok {
				return v
			}
		}
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
	var buf [stringPartsOnStack]storage.Value
	parts, length := append(buf[:0], s), s.StringByteLength()
	for _, a := range args {
		parts, length = appendStringPart(cx, parts, length, cx.ToStringValue(a))
	}
	return cx.ConcatStrings(parts)
}

func stringRepeat(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
	if n*float64(byteLength) > storage.MaxStringLength {
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
}

var (
	nativeStringPadStart = engine.RegisterNative(stringPadNative("padStart", true))
	nativeStringPadEnd   = engine.RegisterNative(stringPadNative("padEnd", false))
)

// stringPadNative returns the method name of String.prototype, which pads a string at its start with
// atStart and at its end otherwise.
//
//go:noinline
func stringPadNative(name string, atStart bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
		if target > storage.MaxStringLength {
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
	}
}

var (
	nativeStringTrim      = engine.RegisterNative(stringTrimNative("trim", true, true))
	nativeStringTrimStart = engine.RegisterNative(stringTrimNative("trimStart", true, false))
	nativeStringTrimEnd   = engine.RegisterNative(stringTrimNative("trimEnd", false, true))
)

// stringTrimNative returns the method name of String.prototype, which trims a string at its start
// with start and at its end with end.
//
//go:noinline
func stringTrimNative(name string, start, end bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		s := thisStringValue(cx, this, name)
		return cx.TrimString(s, start, end)
	}
}

var (
	nativeStringAnchor    = engine.RegisterNative(stringHTMLNative("anchor", "a", "name"))
	nativeStringBig       = engine.RegisterNative(stringHTMLNative("big", "big", ""))
	nativeStringBlink     = engine.RegisterNative(stringHTMLNative("blink", "blink", ""))
	nativeStringBold      = engine.RegisterNative(stringHTMLNative("bold", "b", ""))
	nativeStringFixed     = engine.RegisterNative(stringHTMLNative("fixed", "tt", ""))
	nativeStringFontcolor = engine.RegisterNative(stringHTMLNative("fontcolor", "font", "color"))
	nativeStringFontsize  = engine.RegisterNative(stringHTMLNative("fontsize", "font", "size"))
	nativeStringItalics   = engine.RegisterNative(stringHTMLNative("italics", "i", ""))
	nativeStringLink      = engine.RegisterNative(stringHTMLNative("link", "a", "href"))
	nativeStringSmall     = engine.RegisterNative(stringHTMLNative("small", "small", ""))
	nativeStringStrike    = engine.RegisterNative(stringHTMLNative("strike", "strike", ""))
	nativeStringSub       = engine.RegisterNative(stringHTMLNative("sub", "sub", ""))
	nativeStringSup       = engine.RegisterNative(stringHTMLNative("sup", "sup", ""))
)

// stringHTMLNative returns the method name of String.prototype, which makes an element tag of a
// string. The argument of the method is the value of the attribute attr, unless attr is empty.
//
//go:noinline
func stringHTMLNative(name string, tag, attr wtf8.String) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		// CreateHTML.
		s := thisStringValue(cx, this, name)
		var b wtf8.Buffer
		b.WriteString("<" + tag)
		if attr != "" {
			v := cx.ToString(arg(args, 0))
			b.WriteString(" " + attr + "=\"")
			b.WriteString(wtf8.String(strings.ReplaceAll(string(v), "\"", "&quot;")))
			b.WriteString("\"")
		}
		b.WriteString(">")
		s.WriteStringTo(&b)
		b.WriteString("</" + tag + ">")
		if b.Len() > storage.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
		return cx.NewString(b.String())
	}
}

func stringLocaleCompare(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "localeCompare")
	that := cx.ToStringValue(arg(args, 0))
	if s.IsASCII() && that.IsASCII() {
		// An ASCII string is in every normalization form.
		return storage.Number(float64(storage.CompareStrings(s, that)))
	}
	return storage.Number(float64(wtf8.Compare(mapWellFormed(cx, s.Str(), normalizations[unorm.NFC]), mapWellFormed(cx, that.Str(), normalizations[unorm.NFC]))))
}

func stringNormalize(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "normalize")
	form := unorm.NFC
	if f := arg(args, 0); !f.IsUndefined() {
		switch cx.ToString(f) {
		case "NFC":
		case "NFD":
			form = unorm.NFD
		case "NFKC":
			form = unorm.NFKC
		case "NFKD":
			form = unorm.NFKD
		default:
			cx.ThrowRangeError("the normalization form should be one of NFC, NFD, NFKC, NFKD")
		}
	}
	if s.IsASCII() {
		return s
	}
	str := s.Str()
	if wtf8.IsWellFormed(str) && unorm.QuickCheck(form, string(str), cx.Poll) {
		return s
	}
	return cx.NewString(mapLimited(cx, str, normalizations[form]))
}

func stringIsWellFormed(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Bool(thisStringValue(cx, this, "isWellFormed").IsWellFormed())
}

func stringToWellFormed(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.ToWellFormed(thisStringValue(cx, this, "toWellFormed"))
}

func stringSplit(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cx.RequireObjectCoercible(this, "String.prototype.split")
	sepArg, limArg := arg(args, 0), arg(args, 1)
	if !this.IsString() || limArg.IsObject() {
		sepArg = cx.OwnRegExp(sepArg)
	}
	if sepArg.IsObject() {
		if m := cx.GetMethod(sepArg, cx.WellKnownSymbol(storage.SymbolSplit)); m != nil {
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
		return storage.ObjectValue(cx.NewArray(nil))
	}
	if sepArg.IsUndefined() {
		return storage.ObjectValue(cx.NewArray([]storage.Value{s}))
	}
	if sep.StringByteLength() == 0 {
		n := int(min(uint64(s.StringLength()), uint64(limit)))
		out := make([]storage.Value, n)
		for i := 0; i < n; i += storage.PollInterval {
			if i > 0 {
				cx.Poll()
			}
			unitStrings(cx, out[i:min(i+storage.PollInterval, n)], s, i)
		}
		return storage.ObjectValue(cx.NewArray(out))
	}
	var out []storage.Value
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
	return storage.ObjectValue(cx.NewArray(out))
}

var (
	nativeStringReplace = engine.RegisterNativeWithKernels(stringReplaceNative("replace", false), engine.Kernels{
		Method2:  quiet.StringReplaceKernel,
		Contract: shortContract,
	})
	nativeStringReplaceAll = engine.RegisterNative(stringReplaceNative("replaceAll", true))
)

// stringReplaceNative returns the method name of String.prototype, which replaces all matches with
// all and the first one otherwise.
//
//go:noinline
func stringReplaceNative(name string, all bool) engine.NativeFunc {
	fullName := "String.prototype." + name
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		cx.RequireObjectCoercible(this, fullName)
		pat, rep := arg(args, 0), arg(args, 1)
		if !this.IsString() || rep.IsObject() && !rep.IsCallable() {
			pat = cx.OwnRegExp(pat)
		}
		if pat.IsObject() {
			if all {
				requireGlobalRegExp(cx, pat.Object(), "replaceAll")
			}
			// Where it runs no JavaScript and cannot throw but for a too long result, the original
			// @@replace is done without a native frame.
			if this.IsString() && rep.IsString() {
				if v, ok := replaceUnmodified(cx, pat.Object(), this, rep.Str()); ok {
					return v
				}
			}
			if m := cx.GetMethod(pat, cx.WellKnownSymbol(storage.SymbolReplace)); m != nil {
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
				writeLimited(cx, &b, cx.ToString(cx.Call(rep.Object(), storage.Undefined, search, storage.Number(float64(p)), s)))
			} else {
				writeSubstitution(cx, &b, search.Str(), s, p, substCaptures{}, storage.Undefined, repStr)
			}
			end = p + searchLen
		}
		if end < n {
			writeLimited(cx, &b, s.Substring(end, n))
		}
		return cx.NewString(b.String())
	}
}

var (
	nativeStringMatch    = engine.RegisterNative(stringMatchNative("match", storage.SymbolMatch))
	nativeStringMatchAll = engine.RegisterNative(stringMatchNative("matchAll", storage.SymbolMatchAll))
	nativeStringSearch   = engine.RegisterNative(stringMatchNative("search", storage.SymbolSearch))
)

// stringMatchNative returns the method name of String.prototype, which calls the method key of a
// regular expression.
//
//go:noinline
func stringMatchNative(name string, key storage.WellKnownSymbol) engine.NativeFunc {
	fullName := "String.prototype." + name
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		cx.RequireObjectCoercible(this, fullName)
		pat := arg(args, 0)
		if !this.IsString() {
			pat = cx.OwnRegExp(pat)
		}
		if pat.IsObject() {
			if name == "matchAll" {
				requireGlobalRegExp(cx, pat.Object(), "matchAll")
			}
			if m := cx.GetMethod(pat, cx.WellKnownSymbol(key)); m != nil {
				return cx.Call(m, pat, this)
			}
		}
		s := cx.ToStringValue(this)
		flags := storage.Undefined
		if name == "matchAll" {
			flags = cx.NewString("g")
		}
		rx := regExpCreate(cx, pat, flags)
		return cx.CallValue(cx.GetV(storage.ObjectValue(rx), cx.WellKnownSymbol(key)), storage.ObjectValue(rx), s)
	}
}

func stringIteratorMethod(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisStringValue(cx, this, "[Symbol.iterator]")
	return storage.ObjectValue(cx.NewIteratorObject(cx.Intrinsic(storage.IntrinsicStringIteratorPrototype), &quiet.StringIterator{Str: s.Str()}))
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
	if size < 0 || size > storage.MaxStringLength {
		return 0
	}
	return int(size)
}

// checkStringLength throws a RangeError when n is more bytes than a string may have.
func checkStringLength(cx *engine.ExecutionContext, n int) {
	if n > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
}

// appendStringPart appends the string s to parts, whose strings have length bytes in all, and
// returns the new parts and length. It throws a RangeError instead when the length would pass
// MaxStringLength. The strings of a full parts are first joined into one, so parts never grows
// beyond its capacity.
func appendStringPart(cx *engine.ExecutionContext, parts []storage.Value, length int, s storage.Value) ([]storage.Value, int) {
	length += s.StringByteLength()
	checkStringLength(cx, length)
	if len(parts) == cap(parts) {
		parts = append(parts[:0], cx.ConcatStrings(parts))
	}
	return append(parts, s), length
}

// writeStringLimited writes ToString(v) to b, and throws a RangeError when b gets longer than
// MaxStringLength. A string v that would make b too long is not written.
func writeStringLimited(cx *engine.ExecutionContext, b *wtf8.Buffer, v storage.Value) {
	if v.IsString() {
		checkStringLength(cx, b.Len()+v.StringByteLength())
		v.WriteStringTo(b)
		return
	}
	writeString(cx, b, v)
	checkStringLength(cx, b.Len())
}

// unitStrings sets the elements of out to the strings of the code units of s from the index start
// on.
func unitStrings(cx *engine.ExecutionContext, out []storage.Value, s storage.Value, start int) {
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

// upperMapping is the uppercase mapping without a language, which maps each character on its own.
var upperMapping = stringMapping{
	f: func(s string, start, end int, poll func()) string {
		return xstrings.ToUpperCase(s[start:end])
	},
	boundary: runeBoundary,
}

// lowerMapping is the lowercase mapping without a language.
var lowerMapping = stringMapping{
	f:        xstrings.ToLowerCase,
	boundary: runeBoundary,
}

// upperASCII is the function of the uppercase mapping of ASCII strings.
func upperASCII(s string, start, end int, poll func()) string {
	return strings.ToUpper(s[start:end])
}

// lowerASCII is the function of the lowercase mapping of ASCII strings.
func lowerASCII(s string, start, end int, poll func()) string {
	return strings.ToLower(s[start:end])
}

var (
	nativeStringToUpperCase       = engine.RegisterNative(stringCaseNative("toUpperCase", true, upperASCII, upperMapping))
	nativeStringToLowerCase       = engine.RegisterNative(stringCaseNative("toLowerCase", false, lowerASCII, lowerMapping))
	nativeStringToLocaleUpperCase = engine.RegisterNative(stringCaseNative("toLocaleUpperCase", true, upperASCII, upperMapping))
	nativeStringToLocaleLowerCase = engine.RegisterNative(stringCaseNative("toLocaleLowerCase", false, lowerASCII, lowerMapping))
)

// stringCaseNative returns the method name of String.prototype, which maps a string with ascii when
// it is an ASCII string and with m otherwise.
//
//go:noinline
func stringCaseNative(name string, toUpper bool, ascii func(s string, start, end int, poll func()) string, m stringMapping) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		s := thisStringValue(cx, this, name)
		if mapped, ok := cx.MapASCII(s, toUpper); ok {
			return mapped
		}
		o := s.Object()
		if o.IsASCII() {
			return cx.NewString(wtf8.String(mapInChunks(cx, string(o.Str()), ascii, anyBoundary)))
		}
		return cx.NewString(mapLimited(cx, o.Str(), m))
	}
}

// normalizations are the mappings to the normalization forms.
var normalizations = [...]stringMapping{
	unorm.NFC:  normalization(unorm.NFC),
	unorm.NFD:  normalization(unorm.NFD),
	unorm.NFKC: normalization(unorm.NFKC),
	unorm.NFKD: normalization(unorm.NFKD),
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
