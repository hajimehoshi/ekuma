// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

const (
	uriReserved  = ";/?:@&=+$,"
	uriUnescaped = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"

	// escapeUnescaped is the set of characters that escape leaves as they are.
	escapeUnescaped = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789@*_+-./"

	upperHex = "0123456789ABCDEF"
)

// initURIFunctions defines the URI handling functions and the Annex B functions escape and
// unescape on the global object.
func initURIFunctions(vm *engine.VM) {
	g := vm.GlobalObject().Object()
	uriFunc := func(name string, f func(vm *engine.VM, s string) string) {
		method(vm, g, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return vm.NewString(f(vm, vm.ToString(arg(args, 0))))
		})
	}
	uriFunc("decodeURI", func(vm *engine.VM, s string) string {
		return uriDecode(vm, s, uriReserved+"#")
	})
	uriFunc("decodeURIComponent", func(vm *engine.VM, s string) string {
		return uriDecode(vm, s, "")
	})
	uriFunc("encodeURI", func(vm *engine.VM, s string) string {
		return uriEncode(vm, s, uriReserved+uriUnescaped+"#")
	})
	uriFunc("encodeURIComponent", func(vm *engine.VM, s string) string {
		return uriEncode(vm, s, uriUnescaped)
	})
	uriFunc("escape", func(vm *engine.VM, s string) string {
		return escape(vm, s)
	})
	uriFunc("unescape", func(vm *engine.VM, s string) string {
		return unescape(vm, s)
	})
}

func throwURIError(vm *engine.VM, msg string) {
	vm.Throw(vm.NewError(engine.ErrorKindURI, msg))
}

// uriEncode implements Encode. The ASCII characters in extraUnescaped are not escaped.
func uriEncode(vm *engine.VM, s string, extraUnescaped string) string {
	var i int
	for {
		end := min(len(s), i+uriRun)
		i += unescapedLen(s[i:end], extraUnescaped)
		if i < end || i == len(s) {
			break
		}
		vm.Poll()
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for i < len(s) {
		// A part ends before the first byte of a character.
		n := min(len(s), i+uriRun)
		for n < len(s) && s[n]&0xc0 == 0x80 {
			n++
		}
		uriEncodeRun(vm, &b, s[i:n], extraUnescaped)
		i = n
		vm.Poll()
	}
	return b.String()
}

// uriRun is the number of bytes of a string that the URI functions convert between two checks
// for an interruption.
const uriRun = 1 << 16

// unescapedLen returns the length of the longest prefix of s of the ASCII characters in
// extraUnescaped.
func unescapedLen(s string, extraUnescaped string) int {
	var i int
	for i < len(s) && s[i] < 0x80 && strings.IndexByte(extraUnescaped, s[i]) >= 0 {
		i++
	}
	return i
}

// uriEncodeRun writes s to b as uriEncode encodes it.
func uriEncodeRun(vm *engine.VM, b *strings.Builder, s string, extraUnescaped string) {
	var buf [utf8.UTFMax]byte
	for i := 0; i < len(s); {
		if c := s[i]; c < 0x80 && strings.IndexByte(extraUnescaped, c) >= 0 {
			b.WriteByte(c)
			i++
			continue
		}
		r, size := wtf8.DecodeRune(s[i:])
		i += size
		if wtf8.IsHighSurrogate(r) || wtf8.IsLowSurrogate(r) {
			throwURIError(vm, "URI malformed")
		}
		n := utf8.EncodeRune(buf[:], r)
		for _, o := range buf[:n] {
			b.WriteByte('%')
			b.WriteByte(upperHex[o>>4])
			b.WriteByte(upperHex[o&0xF])
		}
	}
}

// hexByte returns the byte that the two hexadecimal digits at s[i:i+2] denote.
func hexByte(s string, i int) (byte, bool) {
	if i+2 > len(s) {
		return 0, false
	}
	hi, ok1 := hexDigit(s[i])
	lo, ok2 := hexDigit(s[i+1])
	return hi<<4 | lo, ok1 && ok2
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// uriDecode implements Decode. Escapes of the ASCII characters in preserveEscapeSet are kept.
func uriDecode(vm *engine.VM, s string, preserveEscapeSet string) string {
	if strings.IndexByte(s, '%') < 0 {
		return s
	}
	var b wtf8.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		i = uriDecodeRun(vm, &b, s, i, i+uriRun, preserveEscapeSet)
		vm.Poll()
	}
	return b.String()
}

// uriDecodeRun writes s to b from the index i on as uriDecode decodes it, up to about the index
// stop, and returns the index after the part it decoded.
func uriDecodeRun(vm *engine.VM, b *wtf8.Builder, s string, i, stop int, preserveEscapeSet string) int {
	var octets [utf8.UTFMax]byte
	for i < len(s) && i < stop {
		j := strings.IndexByte(s[i:], '%')
		if j < 0 {
			b.WriteString(s[i:])
			return len(s)
		}
		b.WriteString(s[i : i+j])
		i += j
		o, ok := hexByte(s, i+1)
		if !ok {
			throwURIError(vm, "URI malformed")
		}
		if o < 0x80 {
			if strings.IndexByte(preserveEscapeSet, o) >= 0 {
				b.WriteString(s[i : i+3])
			} else {
				_ = b.WriteByte(o)
			}
			i += 3
			continue
		}
		var n int
		switch {
		case o&0xE0 == 0xC0:
			n = 2
		case o&0xF0 == 0xE0:
			n = 3
		case o&0xF8 == 0xF0:
			n = 4
		default:
			throwURIError(vm, "URI malformed")
		}
		octets[0] = o
		i += 3
		for k := 1; k < n; k++ {
			if i >= len(s) || s[i] != '%' {
				throwURIError(vm, "URI malformed")
			}
			o, ok := hexByte(s, i+1)
			if !ok || o&0xC0 != 0x80 {
				throwURIError(vm, "URI malformed")
			}
			octets[k] = o
			i += 3
		}
		r, size := utf8.DecodeRune(octets[:n])
		if r == utf8.RuneError && size <= 1 {
			throwURIError(vm, "URI malformed")
		}
		b.WriteRune(r)
	}
	return i
}

// escape implements the Annex B function escape.
func escape(vm *engine.VM, s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var units []uint16
	for len(s) > 0 {
		// A part ends before the first byte of a character.
		n := min(len(s), escapeRun)
		for n < len(s) && s[n]&0xc0 == 0x80 {
			n++
		}
		units = wtf8.AppendUTF16(units[:0], s[:n])
		escapeUnits(&b, units)
		s = s[n:]
		vm.Poll()
	}
	return b.String()
}

// escapeRun is the number of bytes of a string that escape converts between two checks for an
// interruption.
const escapeRun = 1 << 16

// escapeUnits writes the escapes of the code units us to b.
func escapeUnits(b *strings.Builder, us []uint16) {
	for _, u := range us {
		switch {
		case u < 0x80 && strings.IndexByte(escapeUnescaped, byte(u)) >= 0:
			b.WriteByte(byte(u))
		case u < 0x100:
			b.WriteByte('%')
			b.WriteByte(upperHex[u>>4])
			b.WriteByte(upperHex[u&0xF])
		default:
			b.WriteString("%u")
			b.WriteByte(upperHex[u>>12])
			b.WriteByte(upperHex[u>>8&0xF])
			b.WriteByte(upperHex[u>>4&0xF])
			b.WriteByte(upperHex[u&0xF])
		}
	}
}

// unescape implements the Annex B function unescape.
func unescape(vm *engine.VM, s string) string {
	if strings.IndexByte(s, '%') < 0 {
		return s
	}
	var b wtf8.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		i = unescapeRun(&b, s, i, i+uriRun)
		vm.Poll()
	}
	return b.String()
}

// unescapeRun writes s to b from the index i on as unescape converts it, up to about the index
// stop, and returns the index after the part it converted.
func unescapeRun(b *wtf8.Builder, s string, i, stop int) int {
	for i < len(s) && i < stop {
		j := strings.IndexByte(s[i:], '%')
		if j < 0 {
			b.WriteString(s[i:])
			return len(s)
		}
		b.WriteString(s[i : i+j])
		i += j
		if i+1 < len(s) && s[i+1] == 'u' {
			hi, ok1 := hexByte(s, i+2)
			lo, ok2 := hexByte(s, i+4)
			if ok1 && ok2 {
				b.WriteUnit(uint16(hi)<<8 | uint16(lo))
				i += 6
				continue
			}
		}
		if o, ok := hexByte(s, i+1); ok {
			b.WriteUnit(uint16(o))
			i += 3
			continue
		}
		_ = b.WriteByte('%')
		i++
	}
	return i
}
