// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtins

import (
	"math"
	"strings"
	"unicode"

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
	proto := vm.StringProto
	ctor := constructor(vm, "String", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := vm.EmptyString
		if len(args) > 0 {
			if vm.NewTarget == nil && args[0].IsSymbol() {
				return vm.NewString(vm.DisplayString(args[0]))
			}
			s = vm.ToStringValue(args[0])
		}
		if nt := vm.NewTarget; nt != nil {
			return engine.ObjectValue(vm.NewStringObject(s.Object(), vm.GetPrototypeFromConstructor(nt, vm.StringProto)))
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
				vm.ThrowRangeError("Invalid code point " + vm.ToString(a))
			}
			b.WriteRune(rune(n))
		}
		return vm.NewString(b.String())
	})
	method(vm, ctor, "raw", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cooked := vm.ToObject(arg(args, 0))
		raw := vm.ToObject(vm.GetV(engine.ObjectValue(cooked), vm.K.Raw))
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
			return vm.EmptyString
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
		if v.IsObject() && v.Object().IsRegExpObject() {
			vm.ThrowTypeError("First argument to String.prototype." + what + " must not be a regular expression")
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
		return engine.Bool(strings.HasPrefix(s.Substring(from, n), sub))
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
		return engine.Bool(strings.HasSuffix(s.Substring(0, end), sub))
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
			return vm.EmptyString
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
			return vm.EmptyString
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
			vm.ThrowRangeError("Invalid count value: " + vm.ToString(arg(args, 0)))
		}
		if n == 0 || s.Str() == "" {
			return vm.EmptyString
		}
		if n*float64(len(s.Str())) > engine.MaxStringLength {
			vm.ThrowRangeError("Invalid string length")
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
				vm.ThrowRangeError("Invalid string length")
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
	trim("trimStart", true, false)
	trim("trimEnd", false, true)
	mapCase := func(name string, f func(string) string) {
		method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisString(vm, this, name)
			if s.IsASCII() {
				return vm.NewString(f(s.Str()))
			}
			// Map each well-formed run, leaving lone surrogates as they are.
			var b wtf8.Builder
			str := s.Str()
			for len(str) > 0 {
				i := 0
				for i < len(str) {
					r, size := wtf8.DecodeRune(str[i:])
					if r >= 0xD800 && r <= 0xDFFF {
						break
					}
					i += size
				}
				b.WriteString(f(str[:i]))
				str = str[i:]
				if len(str) > 0 {
					r, size := wtf8.DecodeRune(str)
					b.WriteRune(r)
					str = str[size:]
				}
			}
			return vm.NewString(b.String())
		})
	}
	upper := func(s string) string {
		// The special casing of the German sharp s is the common case that Go's mapping lacks.
		return strings.ReplaceAll(strings.ToUpper(s), "ß", "SS")
	}
	lower := func(s string) string {
		return strings.Map(unicode.ToLower, s)
	}
	mapCase("toUpperCase", upper)
	mapCase("toLowerCase", lower)
	mapCase("toLocaleUpperCase", upper)
	mapCase("toLocaleLowerCase", lower)
	method(vm, proto, "localeCompare", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "localeCompare")
		return engine.Number(float64(wtf8.Compare(s.Str(), vm.ToString(arg(args, 0)))))
	})
	method(vm, proto, "normalize", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "normalize")
		if f := arg(args, 0); !f.IsUndefined() {
			switch vm.ToString(f) {
			case "NFC", "NFD", "NFKC", "NFKD":
			default:
				vm.ThrowRangeError("The normalization form should be one of NFC, NFD, NFKC, NFKD")
			}
		}
		return engine.ObjectValue(s)
	})
	method(vm, proto, "isWellFormed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(wtf8.IsWellFormed(thisString(vm, this, "isWellFormed").Str()))
	})
	method(vm, proto, "toWellFormed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(wtf8.ToValidUTF8(thisString(vm, this, "toWellFormed").Str()))
	})
	method(vm, proto, "split", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.RequireObjectCoercible(this, "String.prototype.split")
		sepArg, limArg := arg(args, 0), arg(args, 1)
		if !sepArg.IsNullish() {
			if m := vm.GetMethod(sepArg, vm.K.SymSplit); m != nil {
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
		var out []engine.Value
		if sep == "" {
			n := s.StringLength()
			for i := 0; i < n && uint32(len(out)) < limit; i++ {
				out = append(out, vm.UnitString(s.UnitAt(i)))
			}
			return engine.ObjectValue(vm.NewArray(out))
		}
		for part := range strings.SplitSeq(s.Str(), sep) {
			if uint32(len(out)) >= limit {
				break
			}
			out = append(out, vm.NewString(part))
		}
		return engine.ObjectValue(vm.NewArray(out))
	})
	replace := func(name string, all bool) {
		method(vm, proto, name, 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			vm.RequireObjectCoercible(this, "String.prototype."+name)
			pat, rep := arg(args, 0), arg(args, 1)
			if !pat.IsNullish() {
				if m := vm.GetMethod(pat, vm.K.SymReplace); m != nil {
					return vm.Call(m, pat, this, rep)
				}
			}
			s := vm.ToStringValue(this).Object()
			search := vm.ToString(pat)
			var repStr string
			if !rep.IsCallable() {
				repStr = vm.ToString(rep)
			}
			var b wtf8.Builder
			pos := 0
			n := s.StringLength()
			searchLen := wtf8.Len(search)
			for {
				i := s.UnitIndexOf(search, pos)
				if i < 0 {
					break
				}
				b.WriteString(s.Substring(min(pos, n), i))
				if rep.IsCallable() {
					b.WriteString(vm.ToString(vm.Call(rep.Object(), engine.Undefined, vm.NewString(search), engine.Number(float64(i)), engine.ObjectValue(s))))
				} else {
					b.WriteString(expandReplacement(repStr, search, s, i, nil))
				}
				pos = i + searchLen
				if !all {
					break
				}
				if searchLen == 0 {
					if pos >= n {
						pos = n + 1
						break
					}
					b.WriteString(s.Substring(pos, pos+1))
					pos++
				}
			}
			if pos < n {
				b.WriteString(s.Substring(pos, n))
			}
			return vm.NewString(b.String())
		})
	}
	replace("replace", false)
	replace("replaceAll", true)
	for _, e := range []struct {
		name string
		key  *engine.Object
	}{
		{"match", vm.K.SymMatch},
		{"matchAll", vm.K.SymMatchAll},
		{"search", vm.K.SymSearch},
	} {
		method(vm, proto, e.name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			vm.RequireObjectCoercible(this, "String.prototype."+e.name)
			pat := arg(args, 0)
			if !pat.IsNullish() {
				if m := vm.GetMethod(pat, e.key); m != nil {
					return vm.Call(m, pat, this)
				}
			}
			vm.Unsupported("RegExp")
			return engine.Undefined
		})
	}
	symMethod(vm, proto, vm.K.SymIterator, "[Symbol.iterator]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisString(vm, this, "[Symbol.iterator]")
		return engine.ObjectValue(vm.NewIteratorObject(vm.StringIteratorProto, &stringIterator{str: s.Str()}))
	})
}

type stringIterator struct {
	str string
	pos int
}

// expandReplacement implements GetSubstitution for a string pattern, and for the captures
// of a regular expression when groups is not nil.
func expandReplacement(rep, matched string, s *engine.Object, pos int, groups []engine.Value) string {
	if !strings.Contains(rep, "$") {
		return rep
	}
	var b wtf8.Builder
	n := s.StringLength()
	for i := 0; i < len(rep); i++ {
		c := rep[i]
		if c != '$' || i+1 >= len(rep) {
			b.WriteString(rep[i : i+1])
			continue
		}
		switch next := rep[i+1]; {
		case next == '$':
			b.WriteString("$")
			i++
		case next == '&':
			b.WriteString(matched)
			i++
		case next == '`':
			b.WriteString(s.Substring(0, pos))
			i++
		case next == '\'':
			if end := pos + wtf8.Len(matched); end < n {
				b.WriteString(s.Substring(end, n))
			}
			i++
		default:
			b.WriteString("$")
		}
	}
	return b.String()
}
