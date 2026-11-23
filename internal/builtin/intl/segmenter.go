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

func (d *segmentsData) boundaries(cx *engine.ExecutionContext) []int32 {
	if d.bounds == nil {
		d.bounds = segment.Boundaries(d.segmenter.kind(), d.runes, d.segmenter.locale, cx.Poll)
	}
	return d.bounds
}

// length returns the number of the code units of the string.
func (d *segmentsData) length() int {
	return int(d.units[len(d.units)-1])
}

// segmentAt returns the start and the end of the segment that contains the code unit n, as
// indices of runes.
func (d *segmentsData) segmentAt(cx *engine.ExecutionContext, n int) (int, int) {
	// The rune that contains the code unit n.
	ri, found := slices.BinarySearch(d.units, int32(n))
	if !found {
		ri--
	}
	bs := d.boundaries(cx)
	k, found := slices.BinarySearch(bs, int32(ri))
	if !found {
		k--
	}
	return int(bs[k]), int(bs[k+1])
}

// segmentData implements CreateSegmentDataObject for the runes from start to end.
func (d *segmentsData) segmentData(cx *engine.ExecutionContext, start, end int) engine.Value {
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
		vals[3] = engine.Bool(isWordLike(cx, d.runes[start:end]))
		propertyCount++
	}
	return engine.ObjectValue(cx.NewObjectFromProperties(keys[:propertyCount], vals[:propertyCount]))
}

// segmentIteratorData holds the internal slots of a Segment Iterator object.
type segmentIteratorData struct {
	segments *segmentsData
	// pos is the index of the rune where the next segment starts.
	pos int
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

// initSegmenter defines Intl.Segmenter.
func (r *realm) initSegmenter(cx *engine.ExecutionContext) {
	realm := cx.Realm()
	proto := cx.NewObject(realm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := cx.NewNativeConstructor("Segmenter", 0, r.newSegmenter)
	cx.Define(ctor, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	cx.Define(r.ns, cx.Intern("Segmenter"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.segmenterProto = proto

	segmentsProto := cx.NewObject(realm.Intrinsic(engine.IntrinsicObjectPrototype))
	iterProto := cx.NewObject(realm.Intrinsic(engine.IntrinsicIteratorPrototype))

	method(cx, ctor, "supportedLocalesOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(cx, args)
	})
	thisSegmenter := func(cx *engine.ExecutionContext, this engine.Value, name string) *segmenterData {
		sd, ok := engine.IntlData(this).(*segmenterData)
		if !ok {
			cx.ThrowTypeError("Intl.Segmenter.prototype." + name + " called on an object that is not an Intl.Segmenter")
		}
		return sd
	}
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
		return engine.ObjectValue(cx.NewIntlObject(segmentsProto, newSegmentsData(cx, sd, str, s)))
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.Segmenter"), engine.FlagConfigurable)

	thisSegments := func(cx *engine.ExecutionContext, this engine.Value, name string) *segmentsData {
		d, ok := engine.IntlData(this).(*segmentsData)
		if !ok {
			cx.ThrowTypeError("%Segments.prototype%." + name + " called on an object that is not a Segments object")
		}
		return d
	}
	method(cx, segmentsProto, "containing", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := thisSegments(cx, this, "containing")
		n := cx.ToIntegerOrInfinity(arg(args, 0))
		if n < 0 || n >= float64(d.length()) {
			return engine.Undefined
		}
		start, end := d.segmentAt(cx, int(n))
		return d.segmentData(cx, start, end)
	})
	cx.Define(segmentsProto, cx.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(cx.NewNative("[Symbol.iterator]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := thisSegments(cx, this, "[Symbol.iterator]")
		return engine.ObjectValue(cx.NewIntlObject(iterProto, &segmentIteratorData{
			segments: d,
		}))
	})), engine.FlagsHidden)

	method(cx, iterProto, "next", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it, ok := engine.IntlData(this).(*segmentIteratorData)
		if !ok {
			cx.ThrowTypeError("%SegmentIteratorPrototype%.next called on an object that is not a Segment Iterator")
		}
		d := it.segments
		if it.pos >= len(d.runes) {
			return cx.CreateIterResultObject(engine.Undefined, true)
		}
		start := it.pos
		bs := d.boundaries(cx)
		k, found := slices.BinarySearch(bs, int32(start))
		if found {
			k++
		}
		end := int(bs[k])
		it.pos = end
		return cx.CreateIterResultObject(d.segmentData(cx, start, end), false)
	})
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Segmenter String Iterator"), engine.FlagConfigurable)
}

// isWordLike is [segment.IsWordLike], checking for an interruption between the parts of a long
// segment. A segment is word-like when any part of it is.
func isWordLike(cx *engine.ExecutionContext, rs []rune) bool {
	for len(rs) > engine.PollInterval {
		if segment.IsWordLike(rs[:engine.PollInterval]) {
			return true
		}
		rs = rs[engine.PollInterval:]
		cx.Poll()
	}
	return segment.IsWordLike(rs)
}
