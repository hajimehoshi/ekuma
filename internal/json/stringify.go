// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// writer is the state of one call of Stringify.
type writer struct {
	vm  *engine.VM
	buf []byte

	keyToJSON  engine.PropKey
	keyRawJSON *engine.Object

	replacer *engine.Object

	// props is the property list of a replacer array, and hasProps says whether there is one.
	props    []engine.PropKey
	hasProps bool

	gap string

	// gapJoins says whether two copies of gap next to each other form a surrogate pair.
	gapJoins bool

	// stack holds the objects being serialized, to detect cycles.
	stack []*engine.Object

	// keys is scratch storage for the keys of the objects being serialized.
	keys []engine.PropKey

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
	stack                   []*engine.Object
	keys                    []engine.PropKey
	getCaches, toJSONCaches []engine.PropertyCache
}

// Stringify implements JSON.stringify.
func (s *Stringifier) Stringify(vm *engine.VM, value, replacer, space engine.Value) engine.Value {
	w := &writer{
		vm:         vm,
		keyToJSON:  engine.NamedKey(vm.Intern("toJSON")),
		keyRawJSON: vm.Intern("rawJSON"),
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
	} else if engine.IsArray(replacer) {
		w.hasProps = true
		r := replacer.Object()
		n := vm.LengthOfArrayLike(r)
		seen := map[engine.PropKey]bool{}
		for i := range n {
			var v engine.Value
			if e, ok := r.Element(i); ok {
				v = e
			} else {
				v = vm.Get(r, indexKey(vm, i))
			}
			var item engine.PropKey
			switch {
			case v.IsString():
				item = vm.ToPropertyKey(v)
			case v.IsNumber():
				item = vm.ToPropertyKey(vm.ToStringValue(v))
			case v.IsObject():
				if _, ok := engine.ThisStringValue(v); ok {
					item = vm.ToPropertyKey(vm.ToStringValue(v))
				} else if _, ok := engine.ThisNumberValue(v); ok {
					item = vm.ToPropertyKey(vm.ToStringValue(v))
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
		if _, ok := engine.ThisNumberValue(space); ok {
			space = engine.Number(vm.ToNumber(space))
		} else if _, ok := engine.ThisStringValue(space); ok {
			space = vm.ToStringValue(space)
		}
	}
	if space.IsNumber() {
		n := min(10, numconv.ToIntegerOrInfinity(space.Number()))
		if n >= 1 {
			w.gap = strings.Repeat(" ", int(n))
		}
	} else if space.IsString() {
		s := space.Object()
		w.gap = s.Substring(0, min(10, s.StringLength()))
		w.gapJoins = wtf8.JoinsPair(w.gap, w.gap)
	}

	var holder *engine.Object
	if w.replacer != nil {
		holder = vm.NewOrdinaryObject()
		vm.CreateDataPropertyOrThrow(holder, engine.NamedKey(vm.Intern("")), value)
	}
	if !w.serialize(holder, engine.NamedKey(vm.Intern("")), value) {
		return engine.Undefined
	}
	w.checkLength()
	return vm.NewString(string(w.buf))
}

func (w *writer) checkLength() {
	if len(w.buf) > engine.MaxStringLength {
		w.vm.ThrowRangeError("invalid string length")
	}
}

// serialize implements SerializeJSONProperty for the value of the property key of holder,
// which is value. It reports false, having written nothing, when the result is undefined.
func (w *writer) serialize(holder *engine.Object, key engine.PropKey, value engine.Value) bool {
	vm := w.vm
	if value.IsObject() || value.IsBigInt() {
		if f := vm.GetVCached(value, w.keyToJSON, cacheAt(&w.toJSONCaches, len(w.stack))); f.IsCallable() {
			value = vm.Call(f.Object(), value, vm.KeyValue(key))
		}
	}
	if w.replacer != nil {
		value = vm.Call(w.replacer, engine.ObjectValue(holder), vm.KeyValue(key), value)
	}
	if value.IsObject() {
		o := value.Object()
		switch {
		case o.IsRawJSON():
			w.buf = append(w.buf, vm.ToString(vm.Get(o, engine.NamedKey(w.keyRawJSON)))...)
			return true
		case isClass(engine.ThisNumberValue(value)):
			value = engine.Number(vm.ToNumber(value))
		case isClass(engine.ThisStringValue(value)):
			value = vm.ToStringValue(value)
		case isClass(engine.ThisBooleanValue(value)):
			b, _ := engine.ThisBooleanValue(value)
			value = engine.Bool(b)
		case isClass(engine.ThisBigIntValue(value)):
			value, _ = engine.ThisBigIntValue(value)
		}
	}
	switch {
	case value.IsNull():
		w.buf = append(w.buf, "null"...)
	case value.IsBool():
		if engine.ToBoolean(value) {
			w.buf = append(w.buf, "true"...)
		} else {
			w.buf = append(w.buf, "false"...)
		}
	case value.IsString():
		w.buf = AppendQuote(w.buf, value.Object().Str())
	case value.IsNumber():
		n := value.Number()
		if math.IsInf(n, 0) || n != n {
			w.buf = append(w.buf, "null"...)
		} else {
			w.buf = numconv.AppendNumber(w.buf, n)
		}
	case value.IsBigInt():
		vm.ThrowTypeError("do not know how to serialize a BigInt")
	case value.IsObject() && !value.IsCallable():
		o := value.Object()
		w.enter(o)
		if engine.IsArray(value) {
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
func (w *writer) enter(o *engine.Object) {
	vm := w.vm
	vm.Poll()
	for _, s := range w.stack {
		if s == o {
			vm.ThrowTypeError("converting circular structure to JSON")
		}
	}
	if len(w.stack) >= maxDepth {
		vm.ThrowRangeError("maximum call stack size exceeded")
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
	var b wtf8.Builder
	for range n {
		b.WriteString(w.gap)
	}
	w.buf = append(w.buf, b.String()...)
}

// serializeObject implements SerializeJSONObject. o is on the stack.
func (w *writer) serializeObject(o *engine.Object) {
	vm := w.vm
	depth := len(w.stack)
	base := len(w.keys)
	if w.hasProps {
		w.keys = append(w.keys, w.props...)
	} else {
		w.keys = vm.EnumerableOwnKeys(o, w.keys)
	}
	end := len(w.keys)
	w.buf = append(w.buf, '{')
	empty := true
	for i := base; i < end; i++ {
		k := w.keys[i]
		v := vm.GetVCached(engine.ObjectValue(o), k, cacheAt(&w.getCaches, i))
		mark := len(w.buf)
		if !empty {
			w.buf = append(w.buf, ',')
		}
		if w.gap != "" {
			w.newline(depth)
		}
		w.buf = AppendQuote(w.buf, vm.KeyString(k))
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
func (w *writer) serializeArray(o *engine.Object) {
	vm := w.vm
	depth := len(w.stack)
	n := vm.LengthOfArrayLike(o)
	w.buf = append(w.buf, '[')
	for i := range n {
		if i > 0 {
			w.buf = append(w.buf, ',')
			if i&0x3ff == 0 {
				vm.Poll()
				w.checkLength()
			}
		}
		if w.gap != "" {
			w.newline(depth)
		}
		var v engine.Value
		if e, ok := o.Element(i); ok {
			v = e
		} else {
			v = vm.Get(o, indexKey(vm, i))
		}
		if !w.serialize(o, indexKey(vm, i), v) {
			w.buf = append(w.buf, "null"...)
		}
	}
	if n > 0 && w.gap != "" {
		w.newline(depth - 1)
	}
	w.buf = append(w.buf, ']')
}
