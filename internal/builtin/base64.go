// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"encoding/base64"
	"encoding/hex"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// decodeResult is the result of FromBase64 and FromHex.
type decodeResult struct {
	// read is the number of code units of the input that were consumed.
	read  int
	bytes []byte

	// err is the message of the SyntaxError, or empty.
	err string
}

// base64Value returns the value of the character c of the standard base64 alphabet, or -1.
func base64Value(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 26
	case c >= '0' && c <= '9':
		return int(c-'0') + 52
	case c == '+':
		return 62
	case c == '/':
		return 63
	}
	return -1
}

// skipASCIIWhitespace implements SkipAsciiWhitespace.
func skipASCIIWhitespace(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case '\t', '\n', '\f', '\r', ' ':
			i++
			continue
		}
		break
	}
	return i
}

// decodeChunk appends the bytes of the base64 chunk of up to four values to dst. When the chunk is shorter,
// it appends only the complete bytes and reports false if strict is set and the discarded bits are not zero.
func decodeChunk(dst []byte, chunk []int, strict bool) ([]byte, bool) {
	var v uint32
	for i := range 4 {
		v <<= 6
		if i < len(chunk) {
			v |= uint32(chunk[i])
		}
	}
	b := [3]byte{byte(v >> 16), byte(v >> 8), byte(v)}
	n := len(chunk) - 1
	if strict && n < 3 && b[n] != 0 {
		return dst, false
	}
	return append(dst, b[:n]...), true
}

// fromBase64 implements FromBase64 for the string s, which may contain any bytes of WTF-8.
func fromBase64(s string, url bool, lastChunkHandling string, maxLength int) decodeResult {
	var r decodeResult
	if maxLength == 0 {
		return r
	}
	chunk := make([]int, 0, 4)
	i := 0
	for {
		i = skipASCIIWhitespace(s, i)
		if i == len(s) {
			if len(chunk) > 0 {
				switch lastChunkHandling {
				case "stop-before-partial":
					return r
				case "loose":
					if len(chunk) == 1 {
						r.err = "the base64 string ends with a single character"
						return r
					}
					r.bytes, _ = decodeChunk(r.bytes, chunk, false)
				default:
					r.err = "the base64 string ends with an incomplete chunk"
					return r
				}
			}
			r.read = len(s)
			return r
		}
		c := s[i]
		i++
		if c == '=' {
			if len(chunk) < 2 {
				r.err = "unexpected padding in the base64 string"
				return r
			}
			i = skipASCIIWhitespace(s, i)
			if len(chunk) == 2 {
				if i == len(s) {
					if lastChunkHandling == "stop-before-partial" {
						return r
					}
					r.err = "incomplete padding in the base64 string"
					return r
				}
				if s[i] == '=' {
					i = skipASCIIWhitespace(s, i+1)
				}
			}
			if i < len(s) {
				r.err = "unexpected characters after the padding of the base64 string"
				return r
			}
			var ok bool
			r.bytes, ok = decodeChunk(r.bytes, chunk, lastChunkHandling == "strict")
			if !ok {
				r.err = "the base64 string has nonzero padding bits"
				return r
			}
			r.read = len(s)
			return r
		}
		if url {
			switch c {
			case '+', '/':
				c = 0
			case '-':
				c = '+'
			case '_':
				c = '/'
			}
		}
		v := base64Value(c)
		if v < 0 {
			r.err = "invalid character in the base64 string"
			return r
		}
		remaining := maxLength - len(r.bytes)
		if remaining == 1 && len(chunk) == 2 || remaining == 2 && len(chunk) == 3 {
			return r
		}
		chunk = append(chunk, v)
		if len(chunk) == 4 {
			r.bytes, _ = decodeChunk(r.bytes, chunk, false)
			chunk = chunk[:0]
			r.read = i
			if len(r.bytes) == maxLength {
				return r
			}
		}
	}
}

// fromHex implements FromHex for the string s.
func fromHex(s string, maxLength int) decodeResult {
	var r decodeResult
	length := len(s)
	if !wtf8.IsASCII(s) {
		length = wtf8.Len(s)
	}
	if length%2 != 0 {
		r.err = "the hex string has an odd length"
		return r
	}
	for r.read < len(s) && len(r.bytes) < maxLength {
		hi, lo := numconv.DigitValue(s[r.read]), numconv.DigitValue(s[r.read+1])
		if hi < 0 || hi >= 16 || lo < 0 || lo >= 16 {
			r.err = "invalid character in the hex string"
			return r
		}
		r.read += 2
		r.bytes = append(r.bytes, byte(hi<<4|lo))
	}
	return r
}

// optionsObject implements GetOptionsObject.
func optionsObject(vm *engine.VM, options engine.Value) engine.Value {
	if options.IsUndefined() {
		return engine.ObjectValue(vm.NewObject(nil))
	}
	if !options.IsObject() {
		vm.ThrowTypeError("options must be an object")
	}
	return options
}

// stringOption returns the option name of opts, which must be one of choices; undefined stands for choices[0].
func stringOption(vm *engine.VM, opts engine.Value, name string, choices ...string) string {
	v := vm.GetV(opts, vm.Intern(name))
	if v.IsUndefined() {
		return choices[0]
	}
	if v.IsString() {
		s := v.Object().Str()
		if slices.Contains(choices, s) {
			return s
		}
	}
	vm.ThrowTypeError("invalid value of the option " + name + ": " + vm.DisplayString(v))
	return ""
}

func initUint8ArrayBase64(vm *engine.VM, bi *binaryIntrinsics) {
	ctor := bi.ctors[engine.ElementUint8]
	proto := bi.protos[engine.ElementUint8]

	// thisUint8Array implements ValidateUint8Array with the access mode write when write is set.
	thisUint8Array := func(vm *engine.VM, this engine.Value, write bool, what string) *engine.TypedArray {
		t := engine.TypedArrayData(this)
		if t == nil || t.ElementType() != engine.ElementUint8 {
			vm.ThrowTypeError("Uint8Array.prototype." + what + ": this is not a Uint8Array")
		}
		if write && t.ArrayBuffer().IsImmutable() {
			vm.ThrowTypeError("Uint8Array.prototype." + what + ": the typed array is backed by an immutable ArrayBuffer")
		}
		return t
	}
	// bytesOf implements GetUint8ArrayBytes.
	bytesOf := func(vm *engine.VM, t *engine.TypedArray, what string) []byte {
		if _, ok := t.Length(); !ok {
			vm.ThrowTypeError("Uint8Array.prototype." + what + ": the typed array is detached or out of bounds")
		}
		return elementsOf(t)
	}
	stringArg := func(vm *engine.VM, args []engine.Value) string {
		s := arg(args, 0)
		if !s.IsString() {
			vm.ThrowTypeError("the argument must be a string")
		}
		return s.Object().Str()
	}
	base64Options := func(vm *engine.VM, options engine.Value) (bool, string) {
		opts := optionsObject(vm, options)
		alphabet := stringOption(vm, opts, "alphabet", "base64", "base64url")
		last := stringOption(vm, opts, "lastChunkHandling", "loose", "strict", "stop-before-partial")
		return alphabet == "base64url", last
	}
	throwSyntaxError := func(vm *engine.VM, msg string) {
		vm.Throw(vm.NewError(engine.ErrorKindSyntax, msg))
	}
	newUint8Array := func(vm *engine.VM, b []byte) engine.Value {
		o := bi.newTypedArray(vm, proto, engine.ElementUint8, int64(len(b)))
		writeElements(engine.TypedArrayData(engine.ObjectValue(o)), 0, b)
		return engine.ObjectValue(o)
	}
	// setResult implements the end of setFromBase64 and setFromHex.
	setResult := func(vm *engine.VM, t *engine.TypedArray, r decodeResult) engine.Value {
		writeElements(t, 0, r.bytes)
		if r.err != "" {
			throwSyntaxError(vm, r.err)
		}
		o := vm.NewOrdinaryObject()
		vm.CreateDataPropertyOrThrow(o, engine.NamedKey(vm.Intern("read")), engine.Number(float64(r.read)))
		vm.CreateDataPropertyOrThrow(o, engine.NamedKey(vm.Intern("written")), engine.Number(float64(len(r.bytes))))
		return engine.ObjectValue(o)
	}

	method(vm, ctor, "fromBase64", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := stringArg(vm, args)
		url, last := base64Options(vm, arg(args, 1))
		r := fromBase64(s, url, last, numconv.MaxSafeInteger)
		if r.err != "" {
			throwSyntaxError(vm, r.err)
		}
		return newUint8Array(vm, r.bytes)
	})
	method(vm, ctor, "fromHex", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := fromHex(stringArg(vm, args), numconv.MaxSafeInteger)
		if r.err != "" {
			throwSyntaxError(vm, r.err)
		}
		return newUint8Array(vm, r.bytes)
	})
	method(vm, proto, "setFromBase64", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisUint8Array(vm, this, true, "setFromBase64")
		s := stringArg(vm, args)
		url, last := base64Options(vm, arg(args, 1))
		b := bytesOf(vm, t, "setFromBase64")
		return setResult(vm, t, fromBase64(s, url, last, len(b)))
	})
	method(vm, proto, "setFromHex", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisUint8Array(vm, this, true, "setFromHex")
		s := stringArg(vm, args)
		b := bytesOf(vm, t, "setFromHex")
		return setResult(vm, t, fromHex(s, len(b)))
	})
	method(vm, proto, "toBase64", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisUint8Array(vm, this, false, "toBase64")
		opts := optionsObject(vm, arg(args, 0))
		alphabet := stringOption(vm, opts, "alphabet", "base64", "base64url")
		omitPadding := engine.ToBoolean(vm.GetV(opts, vm.Intern("omitPadding")))
		b := bytesOf(vm, t, "toBase64")
		enc := base64.StdEncoding
		if alphabet == "base64url" {
			enc = base64.URLEncoding
		}
		if omitPadding {
			enc = enc.WithPadding(base64.NoPadding)
		}
		return vm.NewString(enc.EncodeToString(b))
	})
	method(vm, proto, "toHex", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisUint8Array(vm, this, false, "toHex")
		return vm.NewString(hex.EncodeToString(bytesOf(vm, t, "toHex")))
	})
}
