// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/intl/segment"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// segmenterData holds the internal slots of an Intl.Segmenter object.
type segmenterData struct {
	locale      string
	granularity string
}

func (sd *segmenterData) kind() segment.Granularity {
	switch sd.granularity {
	case "word":
		return segment.Word
	case "sentence":
		return segment.Sentence
	}
	return segment.Grapheme
}

// segmentsData holds the internal slots of a Segments object.
type segmentsData struct {
	segmenter *segmenterData
	str       engine.Value
	runes     []rune
	// units are the code unit indices of the runes, with the length of the string last.
	units []int32
	// bounds are the boundaries of the segments as indices of runes, computed on first use.
	bounds []int32
}

func newSegmentsData(vm *engine.VM, sd *segmenterData, str engine.Value, s wtf8.String) *segmentsData {
	// Every code point starts with a byte that is not a continuation byte.
	var n int
	for i := 0; i < len(s); i++ {
		if s[i]&0xC0 != 0x80 {
			n++
		}
	}
	d := &segmentsData{
		segmenter: sd,
		str:       str,
		runes:     make([]rune, 0, n),
		units:     make([]int32, 0, n+1),
	}
	var u int32
	var p engine.Pacer
	for len(s) > 0 {
		p.Step(vm)
		r, size := wtf8.DecodeRune(s)
		s = s[size:]
		d.runes = append(d.runes, r)
		d.units = append(d.units, u)
		if r > 0xFFFF {
			u += 2
		} else {
			u++
		}
	}
	d.units = append(d.units, u)
	return d
}

func (d *segmentsData) boundaries(vm *engine.VM) []int32 {
	if d.bounds == nil {
		d.bounds = segment.Boundaries(d.segmenter.kind(), d.runes, d.segmenter.locale, vm.Poll)
	}
	return d.bounds
}

// length returns the number of the code units of the string.
func (d *segmentsData) length() int {
	return int(d.units[len(d.units)-1])
}

// segmentAt returns the start and the end of the segment that contains the code unit n, as
// indices of runes.
func (d *segmentsData) segmentAt(vm *engine.VM, n int) (int, int) {
	// The rune that contains the code unit n.
	ri, found := slices.BinarySearch(d.units, int32(n))
	if !found {
		ri--
	}
	bs := d.boundaries(vm)
	k, found := slices.BinarySearch(bs, int32(ri))
	if !found {
		k--
	}
	return int(bs[k]), int(bs[k+1])
}

// segmentData implements CreateSegmentDataObject for the runes from start to end.
func (d *segmentsData) segmentData(vm *engine.VM, start, end int) engine.Value {
	b := make([]byte, 0, int(d.units[end]-d.units[start]))
	var p engine.Pacer
	for _, r := range d.runes[start:end] {
		p.Step(vm)
		b = wtf8.AppendRune(b, r)
	}
	keys := [4]engine.PropKey{
		engine.NamedKey(vm.Intern("segment")),
		engine.NamedKey(vm.Intern("index")),
		engine.NamedKey(vm.Intern("input")),
	}
	vals := [4]engine.Value{vm.NewStringBytes(b), engine.Number(float64(d.units[start])), d.str}
	propertyCount := 3
	if d.segmenter.granularity == "word" {
		keys[3] = engine.NamedKey(vm.Intern("isWordLike"))
		vals[3] = engine.Bool(isWordLike(vm, d.runes[start:end]))
		propertyCount++
	}
	return engine.ObjectValue(vm.NewObjectFromProperties(keys[:propertyCount], vals[:propertyCount]))
}

// segmentIteratorData holds the internal slots of a Segment Iterator object.
type segmentIteratorData struct {
	segments *segmentsData
	// pos is the index of the rune where the next segment starts.
	pos int
}

// newSegmenter implements the Intl.Segmenter constructor.
func (r *realm) newSegmenter(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError("constructor Intl.Segmenter requires 'new'")
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.segmenterProto })
	requested := canonicalizeLocaleList(vm, arg(args, 0))
	// GetOptionsObject.
	var options *engine.Object
	switch o := arg(args, 1); {
	case o.IsUndefined():
		options = vm.NewObject(nil)
	case o.IsObject():
		options = o.Object()
	default:
		vm.ThrowTypeError("the options of Intl.Segmenter must be an object")
	}
	bestFit := localeMatcher(vm, options)
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, nil)
	sd := &segmenterData{
		locale:      res.Locale,
		granularity: getStringOption(vm, options, "granularity", "grapheme", "grapheme", "word", "sentence"),
	}
	return engine.ObjectValue(vm.NewIntlObject(proto, sd))
}

// initSegmenter defines Intl.Segmenter.
func (r *realm) initSegmenter(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("Segmenter", 0, r.newSegmenter)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("Segmenter"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.segmenterProto = proto

	segmentsProto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	iterProto := vm.NewObject(vm.Intrinsic(engine.IntrinsicIteratorPrototype))

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	thisSegmenter := func(vm *engine.VM, this engine.Value, name string) *segmenterData {
		sd, ok := engine.IntlData(this).(*segmenterData)
		if !ok {
			vm.ThrowTypeError("Intl.Segmenter.prototype." + name + " called on an object that is not an Intl.Segmenter")
		}
		return sd
	}
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		sd := thisSegmenter(vm, this, "resolvedOptions")
		out := vm.NewObjectFromProperties(
			[]engine.PropKey{engine.NamedKey(vm.Intern("locale")), engine.NamedKey(vm.Intern("granularity"))},
			[]engine.Value{vm.NewString(wtf8.String(sd.locale)), vm.NewString(wtf8.String(sd.granularity))},
		)
		return engine.ObjectValue(out)
	})
	method(vm, proto, "segment", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		sd := thisSegmenter(vm, this, "segment")
		s := vm.ToString(arg(args, 0))
		str := vm.NewString(s)
		return engine.ObjectValue(vm.NewIntlObject(segmentsProto, newSegmentsData(vm, sd, str, s)))
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.Segmenter"), engine.FlagConfigurable)

	thisSegments := func(vm *engine.VM, this engine.Value, name string) *segmentsData {
		d, ok := engine.IntlData(this).(*segmentsData)
		if !ok {
			vm.ThrowTypeError("%Segments.prototype%." + name + " called on an object that is not a Segments object")
		}
		return d
	}
	method(vm, segmentsProto, "containing", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		d := thisSegments(vm, this, "containing")
		n := vm.ToIntegerOrInfinity(arg(args, 0))
		if n < 0 || n >= float64(d.length()) {
			return engine.Undefined
		}
		start, end := d.segmentAt(vm, int(n))
		return d.segmentData(vm, start, end)
	})
	vm.Define(segmentsProto, vm.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(vm.NewNative("[Symbol.iterator]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		d := thisSegments(vm, this, "[Symbol.iterator]")
		return engine.ObjectValue(vm.NewIntlObject(iterProto, &segmentIteratorData{
			segments: d,
		}))
	})), engine.FlagsHidden)

	method(vm, iterProto, "next", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it, ok := engine.IntlData(this).(*segmentIteratorData)
		if !ok {
			vm.ThrowTypeError("%SegmentIteratorPrototype%.next called on an object that is not a Segment Iterator")
		}
		d := it.segments
		if it.pos >= len(d.runes) {
			return vm.CreateIterResultObject(vm.CurrentRealm(), engine.Undefined, true)
		}
		start := it.pos
		bs := d.boundaries(vm)
		k, found := slices.BinarySearch(bs, int32(start))
		if found {
			k++
		}
		end := int(bs[k])
		it.pos = end
		return vm.CreateIterResultObject(vm.CurrentRealm(), d.segmentData(vm, start, end), false)
	})
	vm.Define(iterProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Segmenter String Iterator"), engine.FlagConfigurable)
}

// isWordLike is [segment.IsWordLike], checking for an interruption between the parts of a long
// segment. A segment is word-like when any part of it is.
func isWordLike(vm *engine.VM, rs []rune) bool {
	for len(rs) > engine.PollInterval {
		if segment.IsWordLike(rs[:engine.PollInterval]) {
			return true
		}
		rs = rs[engine.PollInterval:]
		vm.Poll()
	}
	return segment.IsWordLike(rs)
}
