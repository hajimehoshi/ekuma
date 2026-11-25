// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"encoding/base64"
	"encoding/hex"
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
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

// base64Values holds the value of each byte as a character of the standard base64 alphabet,
// or -1.
var base64Values = func() [256]int8 {
	var t [256]int8
	for c := range t {
		t[c] = int8(base64Value(byte(c)))
	}
	return t
}()

var base64URLValues = func() [256]int8 {
	t := base64Values
	t['-'], t['_'] = 62, 63
	t['+'], t['/'] = -1, -1
	return t
}()

// hexValues holds the value of each byte as a hexadecimal digit, or -1.
var hexValues = func() [256]int8 {
	var t [256]int8
	for c := range t {
		t[c] = -1
		if v := numconv.DigitValue(byte(c)); v >= 0 && v < 16 {
			t[c] = int8(v)
		}
	}
	return t
}()

// skipASCIIWhitespace implements SkipAsciiWhitespace.
func skipASCIIWhitespace(s wtf8.String, i int) int {
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

// decodeRun is the number of bytes of the input that a decoder reads between two checks for an
// interruption.
const decodeRun = 1 << 16

// fromBase64 implements FromBase64 for the string s, which may contain any bytes of WTF-8.
func fromBase64(cx *engine.ExecutionContext, s wtf8.String, url bool, lastChunkHandling wtf8.String, maxLength int) decodeResult {
	var r decodeResult
	if maxLength == 0 {
		return r
	}
	r.bytes = make([]byte, 0, min(maxLength, len(s)/4*3+3))
	values := &base64Values
	if url {
		values = &base64URLValues
	}
	chunk := make([]int, 0, 4)
	var i int
	for {
		var done bool
		i, chunk, done = fromBase64Run(s, i, i+decodeRun, values, lastChunkHandling, maxLength, &r, chunk)
		if done {
			return r
		}
		cx.Poll()
	}
}

// fromBase64Run is fromBase64 from the index i, with the chunk of values read so far, up to about
// the index stop. It returns where it stopped and the chunk then, and reports whether r is the
// result.
func fromBase64Run(s wtf8.String, i, stop int, values *[256]int8, lastChunkHandling wtf8.String, maxLength int, r *decodeResult, chunk []int) (int, []int, bool) {
	for {
		if i >= stop {
			return i, chunk, false
		}
		if len(chunk) == 0 {
			// Whole chunks of four characters of the alphabet decode as the state machine
			// below does while three bytes or more remain.
			for i+4 <= len(s) && i < stop && maxLength-len(r.bytes) >= 3 {
				a, b, c, d := values[s[i]], values[s[i+1]], values[s[i+2]], values[s[i+3]]
				if a|b|c|d < 0 {
					break
				}
				v := uint32(a)<<18 | uint32(b)<<12 | uint32(c)<<6 | uint32(d)
				r.bytes = append(r.bytes, byte(v>>16), byte(v>>8), byte(v))
				i += 4
				r.read = i
			}
			if len(r.bytes) == maxLength {
				return i, chunk, true
			}
		}
		i = skipASCIIWhitespace(s, i)
		if i == len(s) {
			if len(chunk) > 0 {
				switch lastChunkHandling {
				case "stop-before-partial":
					return i, chunk, true
				case "loose":
					if len(chunk) == 1 {
						r.err = "the base64 string ends with a single character"
						return i, chunk, true
					}
					r.bytes, _ = decodeChunk(r.bytes, chunk, false)
				default:
					r.err = "the base64 string ends with an incomplete chunk"
					return i, chunk, true
				}
			}
			r.read = len(s)
			return i, chunk, true
		}
		c := s[i]
		i++
		if c == '=' {
			if len(chunk) < 2 {
				r.err = "unexpected padding in the base64 string"
				return i, chunk, true
			}
			i = skipASCIIWhitespace(s, i)
			if len(chunk) == 2 {
				if i == len(s) {
					if lastChunkHandling == "stop-before-partial" {
						return i, chunk, true
					}
					r.err = "incomplete padding in the base64 string"
					return i, chunk, true
				}
				if s[i] == '=' {
					i = skipASCIIWhitespace(s, i+1)
				}
			}
			if i < len(s) {
				r.err = "unexpected characters after the padding of the base64 string"
				return i, chunk, true
			}
			var ok bool
			r.bytes, ok = decodeChunk(r.bytes, chunk, lastChunkHandling == "strict")
			if !ok {
				r.err = "the base64 string has nonzero padding bits"
				return i, chunk, true
			}
			r.read = len(s)
			return i, chunk, true
		}
		v := int(values[c])
		if v < 0 {
			r.err = "invalid character in the base64 string"
			return i, chunk, true
		}
		remaining := maxLength - len(r.bytes)
		if remaining == 1 && len(chunk) == 2 || remaining == 2 && len(chunk) == 3 {
			return i, chunk, true
		}
		chunk = append(chunk, v)
		if len(chunk) == 4 {
			r.bytes, _ = decodeChunk(r.bytes, chunk, false)
			chunk = chunk[:0]
			r.read = i
			if len(r.bytes) == maxLength {
				return i, chunk, true
			}
		}
	}
}

// fromHex implements FromHex for the string s.
func fromHex(cx *engine.ExecutionContext, s wtf8.String, maxLength int) decodeResult {
	var r decodeResult
	length := len(s)
	if !wtf8.IsASCII(s) {
		length = wtf8.Len(s)
	}
	if length%2 != 0 {
		r.err = "the hex string has an odd length"
		return r
	}
	// A character that is not ASCII starts within the first len(s)/2 pairs of bytes, and none
	// of its bytes is a digit.
	r.bytes = make([]byte, 0, min(maxLength, len(s)/2))
	for r.read < len(s) && len(r.bytes) < maxLength && r.err == "" {
		if r.read > 0 {
			cx.Poll()
		}
		fromHexRun(s, r.read+decodeRun, maxLength, &r)
	}
	return r
}

// fromHexRun is fromHex from the index r.read up to stop.
func fromHexRun(s wtf8.String, stop, maxLength int, r *decodeResult) {
	for r.read < len(s) && r.read < stop && len(r.bytes) < maxLength {
		hi, lo := hexValues[s[r.read]], hexValues[s[r.read+1]]
		if hi|lo < 0 {
			r.err = "invalid character in the hex string"
			return
		}
		r.read += 2
		r.bytes = append(r.bytes, byte(hi<<4|lo))
	}
}

// optionsObject implements GetOptionsObject.
func optionsObject(cx *engine.ExecutionContext, options engine.Value) engine.Value {
	if options.IsUndefined() {
		return storage.ObjectValue(cx.NewObject(nil))
	}
	if !options.IsObject() {
		cx.ThrowTypeError("options must be an object")
	}
	return options
}

// stringOption returns the option name of opts, which must be one of choices; undefined stands for choices[0].
func stringOption(cx *engine.ExecutionContext, opts engine.Value, name string, choices ...wtf8.String) wtf8.String {
	v := cx.GetV(opts, cx.Intern(wtf8.String(name)))
	if v.IsUndefined() {
		return choices[0]
	}
	if v.IsString() {
		s := v.Str()
		if slices.Contains(choices, s) {
			return s
		}
	}
	cx.ThrowTypeError("invalid value of the option " + name + ": " + cx.DisplayString(v))
	return ""
}

// thisUint8Array implements ValidateUint8Array with the access mode write when write is set.
func thisUint8Array(cx *engine.ExecutionContext, this engine.Value, write bool, what string) *engine.TypedArray {
	t := engine.TypedArrayData(this)
	if t == nil || t.ElementType() != element.Uint8 {
		cx.ThrowTypeError("Uint8Array.prototype." + what + ": this is not a Uint8Array")
	}
	if write && t.ArrayBuffer().IsImmutable() {
		cx.ThrowTypeError("Uint8Array.prototype." + what + ": the typed array is backed by an immutable ArrayBuffer")
	}
	return t
}

func requireUint8ArrayInBounds(cx *engine.ExecutionContext, inBounds bool, what string) {
	if !inBounds {
		cx.ThrowTypeError("Uint8Array.prototype." + what + ": the typed array is detached or out of bounds")
	}
}

// uint8ArrayBytes implements GetUint8ArrayBytes.
func uint8ArrayBytes(cx *engine.ExecutionContext, t *engine.TypedArray, what string) []byte {
	_, ok := t.Length()
	requireUint8ArrayInBounds(cx, ok, what)
	return elementsOf(t)
}

func encodedStringArg(cx *engine.ExecutionContext, args []engine.Value) wtf8.String {
	s := arg(args, 0)
	if !s.IsString() {
		cx.ThrowTypeError("the argument must be a string")
	}
	return s.Str()
}

func base64DecodeOptions(cx *engine.ExecutionContext, options engine.Value) (bool, wtf8.String) {
	opts := optionsObject(cx, options)
	alphabet := stringOption(cx, opts, "alphabet", "base64", "base64url")
	last := stringOption(cx, opts, "lastChunkHandling", "loose", "strict", "stop-before-partial")
	return alphabet == "base64url", last
}

// storeDecodeResult implements the end of setFromBase64 and setFromHex.
func storeDecodeResult(cx *engine.ExecutionContext, t *engine.TypedArray, r decodeResult) engine.Value {
	writeElements(t, 0, r.bytes)
	if r.err != "" {
		cx.ThrowSyntaxError(r.err)
	}
	o := cx.NewObjectFromProperties(
		[]engine.PropKey{storage.NamedKey(cx.Intern("read")), storage.NamedKey(cx.Intern("written"))},
		[]engine.Value{storage.Number(float64(r.read)), storage.Number(float64(len(r.bytes)))},
	)
	return storage.ObjectValue(o)
}

// newUint8Array returns a new Uint8Array of the elements b.
func newUint8Array(cx *engine.ExecutionContext, b []byte) engine.Value {
	o := newTypedArray(cx, cx.Intrinsic(engine.IntrinsicUint8ArrayPrototype), element.Uint8, int64(len(b)))
	writeElements(engine.TypedArrayData(storage.ObjectValue(o)), 0, b)
	return storage.ObjectValue(o)
}

var (
	nativeUint8ArrayFromBase64    = engine.RegisterNative(uint8ArrayFromBase64)
	nativeUint8ArrayFromHex       = engine.RegisterNative(uint8ArrayFromHex)
	nativeUint8ArraySetFromBase64 = engine.RegisterNative(uint8ArraySetFromBase64)
	nativeUint8ArraySetFromHex    = engine.RegisterNative(uint8ArraySetFromHex)
	nativeUint8ArrayToBase64      = engine.RegisterNative(uint8ArrayToBase64)
	nativeUint8ArrayToHex         = engine.RegisterNative(uint8ArrayToHex)
)

func initUint8ArrayBase64(cx *engine.ExecutionContext) {
	ctor := cx.Intrinsic(engine.IntrinsicUint8Array)
	proto := cx.Intrinsic(engine.IntrinsicUint8ArrayPrototype)

	method(cx, ctor, "fromBase64", 1, nativeUint8ArrayFromBase64)
	method(cx, ctor, "fromHex", 1, nativeUint8ArrayFromHex)
	method(cx, proto, "setFromBase64", 1, nativeUint8ArraySetFromBase64)
	method(cx, proto, "setFromHex", 1, nativeUint8ArraySetFromHex)
	method(cx, proto, "toBase64", 0, nativeUint8ArrayToBase64)
	method(cx, proto, "toHex", 0, nativeUint8ArrayToHex)
}

func uint8ArrayFromBase64(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := encodedStringArg(cx, args)
	url, last := base64DecodeOptions(cx, arg(args, 1))
	r := fromBase64(cx, s, url, last, math.MaxInt)
	if r.err != "" {
		cx.ThrowSyntaxError(r.err)
	}
	return newUint8Array(cx, r.bytes)
}

func uint8ArrayFromHex(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := fromHex(cx, encodedStringArg(cx, args), math.MaxInt)
	if r.err != "" {
		cx.ThrowSyntaxError(r.err)
	}
	return newUint8Array(cx, r.bytes)
}

func uint8ArraySetFromBase64(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisUint8Array(cx, this, true, "setFromBase64")
	s := encodedStringArg(cx, args)
	url, last := base64DecodeOptions(cx, arg(args, 1))
	n, ok := t.Length()
	requireUint8ArrayInBounds(cx, ok, "setFromBase64")
	return storeDecodeResult(cx, t, fromBase64(cx, s, url, last, n))
}

func uint8ArraySetFromHex(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisUint8Array(cx, this, true, "setFromHex")
	s := encodedStringArg(cx, args)
	n, ok := t.Length()
	requireUint8ArrayInBounds(cx, ok, "setFromHex")
	return storeDecodeResult(cx, t, fromHex(cx, s, n))
}

func uint8ArrayToBase64(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisUint8Array(cx, this, false, "toBase64")
	opts := optionsObject(cx, arg(args, 0))
	alphabet := stringOption(cx, opts, "alphabet", "base64", "base64url")
	omitPadding := storage.ToBoolean(cx.GetV(opts, cx.Intern("omitPadding")))
	b := uint8ArrayBytes(cx, t, "toBase64")
	enc := base64.StdEncoding
	if alphabet == "base64url" {
		enc = base64.URLEncoding
	}
	if omitPadding {
		enc = enc.WithPadding(base64.NoPadding)
	}
	// Every chunk but the last is a whole number of 3-byte groups, which encode without
	// padding.
	return cx.NewString(wtf8.String(encodeInChunks(cx, b, 3, enc.EncodedLen, enc.AppendEncode)))
}

func uint8ArrayToHex(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisUint8Array(cx, this, false, "toHex")
	return cx.NewString(wtf8.String(encodeInChunks(cx, uint8ArrayBytes(cx, t, "toHex"), 1, hex.EncodedLen, hex.AppendEncode)))
}

// encodeChunk is the number of bytes that encodeInChunks encodes between two checks for an
// interruption.
const encodeChunk = 1 << 20

// encodeInChunks returns the encoding of b by appendEncode, whose length encodedLen returns,
// encoding a multiple of group bytes at a time.
func encodeInChunks(cx *engine.ExecutionContext, b []byte, group int, encodedLen func(n int) int, appendEncode func(dst, src []byte) []byte) string {
	size := encodedLen(len(b))
	checkStringLength(cx, size)
	out := make([]byte, 0, size)
	n := encodeChunk / group * group
	for len(b) > n {
		out = appendEncode(out, b[:n])
		b = b[n:]
		cx.Poll()
	}
	return string(appendEncode(out, b))
}
