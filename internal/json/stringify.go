// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// writer is the state of one call of Stringify.
type writer struct {
	cx  *engine.ExecutionContext
	buf []byte

	keyToJSON storage.PropKey

	replacer *storage.Object

	// props is the property list of a replacer array, and hasProps says whether there is one.
	props    []storage.PropKey
	hasProps bool

	gap wtf8.String

	// gapJoins says whether two copies of gap next to each other form a surrogate pair.
	gapJoins bool

	// stack holds the objects being serialized, to detect cycles.
	stack []*storage.Object

	// entered is the number of objects entered, which paces the checks for an interruption.
	entered int

	// keys is scratch storage for the keys of the objects being serialized.
	keys []storage.PropKey

	// getCaches are the caches for reading the properties whose keys are in keys, by index in keys.
	// Objects of the same layout that are serialized one after another take the same indices.
	getCaches []engine.PropertyCache

	// toJSONCaches are the caches for looking up toJSON, by nesting depth.
	toJSONCaches []engine.PropertyCache
}

// cacheAt returns the cache i of caches, growing caches as needed.
func cacheAt(caches *[]engine.PropertyCache, i int) *engine.PropertyCache {
	for len(*caches) <= i {
		*caches = append(*caches, engine.PropertyCache{})
	}
	return &(*caches)[i]
}

// Stringifier converts values to JSON text. The zero value is ready to use.
// A Stringifier keeps its output buffer between calls.
type Stringifier struct {
	out  []byte
	busy bool

	// The scratch storage of the writer, kept without references between calls.
	stack                   []*storage.Object
	keys                    []storage.PropKey
	getCaches, toJSONCaches []engine.PropertyCache
}

// Stringify implements JSON.stringify.
func (s *Stringifier) Stringify(cx *engine.ExecutionContext, value, replacer, space storage.Value) storage.Value {
	w := &writer{
		cx:        cx,
		keyToJSON: storage.NamedKey(cx.Key(storage.KeyToJSON)),
	}
	// A call from a toJSON method or a replacer during another call uses storage of its own.
	if !s.busy {
		s.busy = true
		w.buf = s.out[:0]
		w.stack = s.stack[:0]
		w.keys = s.keys[:0]
		w.getCaches = s.getCaches[:0]
		w.toJSONCaches = s.toJSONCaches[:0]
		defer func() {
			s.busy = false
			if cap(w.buf) <= maxRetainedBuffer {
				s.out = w.buf[:0]
			} else {
				s.out = nil
			}
			s.stack = releaseScratch(w.stack)
			s.keys = releaseScratch(w.keys)
			s.getCaches = releaseScratch(w.getCaches)
			s.toJSONCaches = releaseScratch(w.toJSONCaches)
		}()
	}

	if replacer.IsCallable() {
		w.replacer = replacer.Object()
	} else if cx.IsArray(replacer) {
		w.hasProps = true
		r := replacer.Object()
		n := cx.LengthOfArrayLike(r)
		seen := map[storage.PropKey]bool{}
		for i := range n {
			var v storage.Value
			if e, ok := storage.Element(r, i); ok {
				cx.PollAt(i)
				v = e
			} else {
				v = cx.Get(r, indexKey(cx, i))
			}
			var item storage.PropKey
			switch {
			case v.IsString():
				item = cx.ToPropertyKey(v)
			case v.IsNumber():
				item = cx.ToPropertyKey(cx.ToStringValue(v))
			case v.IsObject():
				if _, ok := storage.ThisStringValue(v); ok {
					item = cx.ToPropertyKey(cx.ToStringValue(v))
				} else if _, ok := storage.ThisNumberValue(v); ok {
					item = cx.ToPropertyKey(cx.ToStringValue(v))
				} else {
					continue
				}
			default:
				continue
			}
			if !seen[item] {
				seen[item] = true
				w.props = append(w.props, item)
			}
		}
	}

	if space.IsObject() {
		if _, ok := storage.ThisNumberValue(space); ok {
			space = storage.Number(cx.ToNumber(space))
		} else if _, ok := storage.ThisStringValue(space); ok {
			space = cx.ToStringValue(space)
		}
	}
	if space.IsNumber() {
		n := min(10, numconv.ToIntegerOrInfinity(space.Number()))
		if n >= 1 {
			w.gap = wtf8.String(strings.Repeat(" ", int(n)))
		}
	} else if space.IsString() {
		s := space.Object()
		w.gap = s.Substring(0, min(10, s.StringLength()))
		w.gapJoins = wtf8.JoinsPair(w.gap, w.gap)
	}

	var holder *storage.Object
	emptyKey := storage.NamedKey(cx.Intern(""))
	if w.replacer != nil {
		holder = cx.NewOrdinaryObject()
		cx.CreateDataPropertyOrThrow(holder, emptyKey, value)
	}
	if !w.serialize(holder, emptyKey, value) {
		return storage.Undefined
	}
	w.checkLength()
	return cx.NewStringBytes(w.buf)
}

func (w *writer) checkLength() {
	if len(w.buf) > storage.MaxStringLength {
		w.cx.ThrowRangeError("invalid string length")
	}
}

// quoteChunk is the number of bytes of a string that appendQuote escapes between two checks for
// an interruption.
const quoteChunk = 1 << 20

// appendQuote is [AppendQuote] to the output.
func (w *writer) appendQuote(s wtf8.String) {
	if len(s) <= quoteChunk {
		w.buf = AppendQuote(w.buf, s)
		return
	}
	w.buf = append(w.buf, '"')
	for len(s) > 0 {
		// A chunk ends before the first byte of a character.
		n := min(quoteChunk, len(s))
		for n < len(s) && s[n]&0xc0 == 0x80 {
			n++
		}
		w.buf = appendEscaped(w.buf, s[:n])
		s = s[n:]
		w.cx.Poll()
		w.checkLength()
	}
	w.buf = append(w.buf, '"')
}

// serialize implements SerializeJSONProperty for the value of the property key of holder,
// which is value. It reports false, having written nothing, when the result is undefined.
func (w *writer) serialize(holder *storage.Object, key storage.PropKey, value storage.Value) bool {
	cx := w.cx
	if value.IsObject() || value.IsBigInt() {
		if f := cx.GetVCached(value, w.keyToJSON, cacheAt(&w.toJSONCaches, len(w.stack))); f.IsCallable() {
			value = cx.Call(f.Object(), value, cx.KeyValue(key))
		}
	}
	if w.replacer != nil {
		value = cx.Call(w.replacer, storage.ObjectValue(holder), cx.KeyValue(key), value)
	}
	if value.IsObject() {
		o := value.Object()
		switch {
		case o.IsRawJSON():
			w.buf = append(w.buf, cx.ToString(cx.Get(o, storage.NamedKey(cx.Intern("rawJSON"))))...)
			return true
		case isClass(storage.ThisNumberValue(value)):
			value = storage.Number(cx.ToNumber(value))
		case isClass(storage.ThisStringValue(value)):
			value = cx.ToStringValue(value)
		case isClass(storage.ThisBooleanValue(value)):
			b, _ := storage.ThisBooleanValue(value)
			value = storage.Bool(b)
		case isClass(storage.ThisBigIntValue(value)):
			value, _ = storage.ThisBigIntValue(value)
		}
	}
	switch {
	case value.IsNull():
		w.buf = append(w.buf, "null"...)
	case value.IsBool():
		if storage.ToBoolean(value) {
			w.buf = append(w.buf, "true"...)
		} else {
			w.buf = append(w.buf, "false"...)
		}
	case value.IsString():
		if value.StringByteLength() <= shortstrings.MaxBytes {
			var buf [shortstrings.MaxBytes]byte
			w.appendQuote(wtf8.String(value.AppendStringBytes(buf[:0])))
		} else {
			w.appendQuote(value.Str())
		}
	case value.IsNumber():
		n := value.Number()
		if math.IsInf(n, 0) || n != n {
			w.buf = append(w.buf, "null"...)
		} else {
			w.buf = numconv.AppendNumber(w.buf, n)
		}
	case value.IsBigInt():
		cx.ThrowTypeError("do not know how to serialize a BigInt")
	case value.IsObject() && !value.IsCallable():
		o := value.Object()
		w.enter(o)
		if cx.IsArray(value) {
			w.serializeArray(o)
		} else {
			w.serializeObject(o)
		}
		w.stack = w.stack[:len(w.stack)-1]
	default:
		return false
	}
	return true
}

func isClass[T any](_ T, ok bool) bool {
	return ok
}

// enter pushes o on the stack of objects being serialized, and throws for a cycle or too deep a nesting.
func (w *writer) enter(o *storage.Object) {
	cx := w.cx
	w.entered++
	if w.entered&0x3ff == 0 {
		cx.Poll()
	}
	for _, s := range w.stack {
		if s == o {
			cx.ThrowTypeError("converting circular structure to JSON")
		}
	}
	if len(w.stack) >= maxDepth {
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	w.stack = append(w.stack, o)
	w.checkLength()
}

// newline starts a new line indented for the nesting depth n, when there is a gap.
func (w *writer) newline(n int) {
	w.buf = append(w.buf, '\n')
	if !w.gapJoins {
		for range n {
			w.buf = append(w.buf, w.gap...)
		}
		return
	}
	var b wtf8.Buffer
	for range n {
		b.WriteString(w.gap)
	}
	w.buf = append(w.buf, b.String()...)
}

// serializeObject implements SerializeJSONObject. o is on the stack.
func (w *writer) serializeObject(o *storage.Object) {
	cx := w.cx
	depth := len(w.stack)
	base := len(w.keys)
	if w.hasProps {
		w.keys = append(w.keys, w.props...)
	} else {
		w.keys = cx.EnumerableOwnKeys(o, w.keys)
	}
	end := len(w.keys)
	w.buf = append(w.buf, '{')
	empty := true
	for i := base; i < end; i++ {
		if j := i - base; j > 0 && j&0x3ff == 0 {
			cx.Poll()
			w.checkLength()
		}
		k := w.keys[i]
		v := cx.GetVCached(storage.ObjectValue(o), k, cacheAt(&w.getCaches, i))
		mark := len(w.buf)
		if !empty {
			w.buf = append(w.buf, ',')
		}
		if w.gap != "" {
			w.newline(depth)
		}
		w.buf = AppendQuote(w.buf, cx.KeyString(k))
		w.buf = append(w.buf, ':')
		if w.gap != "" {
			w.buf = append(w.buf, ' ')
		}
		if w.serialize(o, k, v) {
			empty = false
		} else {
			w.buf = w.buf[:mark]
		}
	}
	clear(w.keys[base:end])
	w.keys = w.keys[:base]
	if !empty && w.gap != "" {
		w.newline(depth - 1)
	}
	w.buf = append(w.buf, '}')
}

// serializeArray implements SerializeJSONArray. o is on the stack.
func (w *writer) serializeArray(o *storage.Object) {
	cx := w.cx
	depth := len(w.stack)
	n := cx.LengthOfArrayLike(o)
	w.buf = append(w.buf, '[')
	for i := range n {
		if i > 0 {
			w.buf = append(w.buf, ',')
			if i&0x3ff == 0 {
				cx.Poll()
				w.checkLength()
			}
		}
		if w.gap != "" {
			w.newline(depth)
		}
		var v storage.Value
		if e, ok := storage.Element(o, i); ok {
			v = e
		} else {
			v = cx.Get(o, indexKey(cx, i))
		}
		if !w.serialize(o, indexKey(cx, i), v) {
			w.buf = append(w.buf, "null"...)
		}
	}
	if n > 0 && w.gap != "" {
		w.newline(depth - 1)
	}
	w.buf = append(w.buf, ']')
}
