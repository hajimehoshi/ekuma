// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"sort"

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
	units []int
	// bounds are the boundaries of the segments as indices of runes, computed on first use.
	bounds []int
}

func newSegmentsData(sd *segmenterData, str engine.Value, s string) *segmentsData {
	d := &segmentsData{
		segmenter: sd,
		str:       str,
	}
	u := 0
	for len(s) > 0 {
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

func (d *segmentsData) boundaries() []int {
	if d.bounds == nil {
		d.bounds = segment.Boundaries(d.segmenter.kind(), d.runes)
	}
	return d.bounds
}

// length returns the number of the code units of the string.
func (d *segmentsData) length() int {
	return d.units[len(d.units)-1]
}

// segmentAt returns the start and the end of the segment that contains the code unit n, as
// indices of runes.
func (d *segmentsData) segmentAt(n int) (int, int) {
	// The rune that contains the code unit n.
	ri := sort.SearchInts(d.units, n+1) - 1
	bs := d.boundaries()
	k := sort.SearchInts(bs, ri+1) - 1
	return bs[k], bs[k+1]
}

// segmentData implements CreateSegmentDataObject for the runes from start to end.
func (d *segmentsData) segmentData(vm *engine.VM, start, end int) engine.Value {
	out := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	set := func(name string, v engine.Value) {
		vm.CreateDataPropertyOrThrow(out, engine.NamedKey(vm.Intern(name)), v)
	}
	var b []byte
	for _, r := range d.runes[start:end] {
		b = wtf8.AppendRune(b, r)
	}
	set("segment", vm.NewString(string(b)))
	set("index", engine.Number(float64(d.units[start])))
	set("input", d.str)
	if d.segmenter.granularity == "word" {
		set("isWordLike", engine.Bool(segment.IsWordLike(d.runes[start:end])))
	}
	return engine.ObjectValue(out)
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
		out := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
		vm.CreateDataPropertyOrThrow(out, engine.NamedKey(vm.Intern("locale")), vm.NewString(sd.locale))
		vm.CreateDataPropertyOrThrow(out, engine.NamedKey(vm.Intern("granularity")), vm.NewString(sd.granularity))
		return engine.ObjectValue(out)
	})
	method(vm, proto, "segment", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		sd := thisSegmenter(vm, this, "segment")
		s := vm.ToString(arg(args, 0))
		str := vm.NewString(s)
		return engine.ObjectValue(vm.NewIntlObject(segmentsProto, newSegmentsData(sd, str, s)))
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
		start, end := d.segmentAt(int(n))
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
			return vm.CreateIterResultObject(engine.Undefined, true)
		}
		start := it.pos
		bs := d.boundaries()
		k := sort.SearchInts(bs, start+1)
		end := bs[k]
		it.pos = end
		return vm.CreateIterResultObject(d.segmentData(vm, start, end), false)
	})
	vm.Define(iterProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Segmenter String Iterator"), engine.FlagConfigurable)
}
