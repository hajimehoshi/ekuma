// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"slices"
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
	units []int32
	// bounds are computed on first use.
	bounds []segment.Boundary
}

func newSegmentsData(cx *engine.ExecutionContext, sd *segmenterData, str engine.Value, s wtf8.String) *segmentsData {
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
		p.Step(cx)
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

func (d *segmentsData) boundaries(cx *engine.ExecutionContext) []segment.Boundary {
	if d.bounds == nil {
		d.bounds = segment.Boundaries(d.segmenter.kind(), d.runes, d.segmenter.locale, cx.Poll)
	}
	return d.bounds
}

// length returns the number of the code units of the string.
func (d *segmentsData) length() int {
	return int(d.units[len(d.units)-1])
}

// segmentAt returns the index of the segment that contains the code unit n.
func (d *segmentsData) segmentAt(cx *engine.ExecutionContext, n int) int {
	// The rune that contains the code unit n.
	ri, found := slices.BinarySearch(d.units, int32(n))
	if !found {
		ri--
	}
	bs := d.boundaries(cx)
	return sort.Search(len(bs), func(i int) bool { return bs[i].Index() > int32(ri) }) - 1
}

// segmentData implements CreateSegmentDataObject for segment k.
func (d *segmentsData) segmentData(cx *engine.ExecutionContext, k int) engine.Value {
	start, end := d.bounds[k].Index(), d.bounds[k+1].Index()
	b := make([]byte, 0, int(d.units[end]-d.units[start]))
	var p engine.Pacer
	for _, r := range d.runes[start:end] {
		p.Step(cx)
		b = wtf8.AppendRune(b, r)
	}
	keys := [4]engine.PropKey{
		engine.NamedKey(cx.Intern("segment")),
		engine.NamedKey(cx.Intern("index")),
		engine.NamedKey(cx.Intern("input")),
	}
	vals := [4]engine.Value{cx.NewStringBytes(b), engine.Number(float64(d.units[start])), d.str}
	propertyCount := 3
	if d.segmenter.granularity == "word" {
		keys[3] = engine.NamedKey(cx.Intern("isWordLike"))
		vals[3] = engine.Bool(d.bounds[k+1].IsWordLike())
		propertyCount++
	}
	return engine.ObjectValue(cx.NewObjectFromProperties(keys[:propertyCount], vals[:propertyCount]))
}

// segmentIteratorData holds the internal slots of a Segment Iterator object.
type segmentIteratorData struct {
	segments *segmentsData
	// next is the index of the next segment.
	next int
}

// newSegmenter implements the Intl.Segmenter constructor.
func (r *realm) newSegmenter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.Segmenter requires 'new'")
	}
	proto := protoFor(cx, nt, func(r *realm) *engine.Object { return r.segmenterProto })
	requested := canonicalizeLocaleList(cx, arg(args, 0))
	// GetOptionsObject.
	var options *engine.Object
	switch o := arg(args, 1); {
	case o.IsUndefined():
		options = cx.NewObject(nil)
	case o.IsObject():
		options = o.Object()
	default:
		cx.ThrowTypeError("the options of Intl.Segmenter must be an object")
	}
	bestFit := localeMatcher(cx, options)
	res := locale.ResolveLocale(requested, defaultLocale(cx), bestFit, nil)
	sd := &segmenterData{
		locale:      res.Locale,
		granularity: getStringOption(cx, options, "granularity", "grapheme", "grapheme", "word", "sentence"),
	}
	return engine.ObjectValue(cx.NewIntlObject(proto, sd))
}

func thisSegmenter(cx *engine.ExecutionContext, this engine.Value, name string) *segmenterData {
	sd, ok := engine.IntlData(this).(*segmenterData)
	if !ok {
		cx.ThrowTypeError("Intl.Segmenter.prototype." + name + " called on an object that is not an Intl.Segmenter")
	}
	return sd
}

func thisSegments(cx *engine.ExecutionContext, this engine.Value, name string) *segmentsData {
	d, ok := engine.IntlData(this).(*segmentsData)
	if !ok {
		cx.ThrowTypeError("%Segments.prototype%." + name + " called on an object that is not a Segments object")
	}
	return d
}

// initSegmenter defines Intl.Segmenter.
func (r *realm) initSegmenter(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := cx.NewNativeConstructor("Segmenter", 0, r.newSegmenter)
	cx.Define(ctor, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	cx.Define(r.ns, cx.Intern("Segmenter"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.segmenterProto = proto

	segmentsProto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	r.segmentsProto = segmentsProto
	iterProto := cx.NewObject(cx.Intrinsic(engine.IntrinsicIteratorPrototype))
	r.segmentIteratorProto = iterProto

	method(cx, ctor, "supportedLocalesOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(cx, args)
	})
	method(cx, proto, "resolvedOptions", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		sd := thisSegmenter(cx, this, "resolvedOptions")
		out := cx.NewObjectFromProperties(
			[]engine.PropKey{engine.NamedKey(cx.Intern("locale")), engine.NamedKey(cx.Intern("granularity"))},
			[]engine.Value{cx.NewString(wtf8.String(sd.locale)), cx.NewString(wtf8.String(sd.granularity))},
		)
		return engine.ObjectValue(out)
	})
	method(cx, proto, "segment", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		sd := thisSegmenter(cx, this, "segment")
		s := cx.ToString(arg(args, 0))
		str := cx.NewString(s)
		return engine.ObjectValue(cx.NewIntlObject(cx.RealmExtension().(*realm).segmentsProto, newSegmentsData(cx, sd, str, s)))
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.Segmenter"), engine.FlagConfigurable)

	method(cx, segmentsProto, "containing", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := thisSegments(cx, this, "containing")
		n := cx.ToIntegerOrInfinity(arg(args, 0))
		if n < 0 || n >= float64(d.length()) {
			return engine.Undefined
		}
		return d.segmentData(cx, d.segmentAt(cx, int(n)))
	})
	cx.Define(segmentsProto, cx.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(cx.NewNative("[Symbol.iterator]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := thisSegments(cx, this, "[Symbol.iterator]")
		return engine.ObjectValue(cx.NewIntlObject(cx.RealmExtension().(*realm).segmentIteratorProto, &segmentIteratorData{
			segments: d,
		}))
	})), engine.FlagsHidden)

	method(cx, iterProto, "next", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it, ok := engine.IntlData(this).(*segmentIteratorData)
		if !ok {
			cx.ThrowTypeError("%SegmentIteratorPrototype%.next called on an object that is not a Segment Iterator")
		}
		d := it.segments
		if it.next+1 >= len(d.boundaries(cx)) {
			return cx.CreateIterResultObject(engine.Undefined, true)
		}
		v := d.segmentData(cx, it.next)
		it.next++
		return cx.CreateIterResultObject(v, false)
	})
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Segmenter String Iterator"), engine.FlagConfigurable)
}
