// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"slices"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/intl/segment"
	"github.com/hajimehoshi/ekuma/internal/storage"
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
	str       storage.Value
	runes     []rune
	// units are the code unit indices of the runes, with the length of the string last.
	units []int32
	// bounds are computed on first use.
	bounds []segment.Boundary
}

func newSegmentsData(cx *engine.ExecutionContext, sd *segmenterData, str storage.Value, s wtf8.String) *segmentsData {
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
func (d *segmentsData) segmentData(cx *engine.ExecutionContext, k int) storage.Value {
	start, end := d.bounds[k].Index(), d.bounds[k+1].Index()
	b := make([]byte, 0, int(d.units[end]-d.units[start]))
	var p engine.Pacer
	for _, r := range d.runes[start:end] {
		p.Step(cx)
		b = wtf8.AppendRune(b, r)
	}
	keys := [4]storage.PropKey{
		storage.NamedKey(cx.Intern("segment")),
		storage.NamedKey(cx.Intern("index")),
		storage.NamedKey(cx.Intern("input")),
	}
	vals := [4]storage.Value{cx.NewStringBytes(b), storage.Number(float64(d.units[start])), d.str}
	propertyCount := 3
	if d.segmenter.granularity == "word" {
		keys[3] = storage.NamedKey(cx.Intern("isWordLike"))
		vals[3] = storage.Bool(d.bounds[k+1].IsWordLike())
		propertyCount++
	}
	return storage.ObjectValue(cx.NewObjectFromProperties(keys[:propertyCount], vals[:propertyCount]))
}

// segmentIteratorData holds the internal slots of a Segment Iterator object.
type segmentIteratorData struct {
	segments *segmentsData
	// next is the index of the next segment.
	next int
}

// newSegmenter implements the Intl.Segmenter constructor.
func newSegmenter(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.Segmenter requires 'new'")
	}
	proto := cx.GetPrototypeFromConstructor(nt, storage.IntrinsicIntlSegmenterPrototype)
	requested := canonicalizeLocaleList(cx, arg(args, 0))
	// GetOptionsObject.
	var options *storage.Object
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
	return storage.ObjectValue(cx.NewIntlObject(proto, sd))
}

func requireSegmenter(cx *engine.ExecutionContext, sd *segmenterData, name string) {
	if sd == nil {
		cx.ThrowTypeError("Intl.Segmenter.prototype." + name + " called on an object that is not an Intl.Segmenter")
	}
}

func requireSegments(cx *engine.ExecutionContext, d *segmentsData, name string) {
	if d == nil {
		cx.ThrowTypeError("%Segments.prototype%." + name + " called on an object that is not a Segments object")
	}
}

var (
	nativeNewSegmenter             = engine.RegisterNative(newSegmenter)
	nativeSegmenterResolvedOptions = engine.RegisterNative(segmenterResolvedOptions)
	nativeSegmenterSegment         = engine.RegisterNative(segmenterSegment)
	nativeSegmentsContaining       = engine.RegisterNative(segmentsContaining)
	nativeSegmentsIterator         = engine.RegisterNative(segmentsIterator)
	nativeSegmentIteratorNext      = engine.RegisterNative(segmentIteratorNext)
)

// initSegmenter defines Intl.Segmenter.
func initSegmenter(cx *engine.ExecutionContext, ns *storage.Object) {
	ctor, proto := constructor(cx, ns, "Segmenter", 0, storage.IntrinsicIntlSegmenterPrototype, nativeNewSegmenter)

	segmentsProto := cx.NewObject(cx.Intrinsic(storage.IntrinsicObjectPrototype))
	cx.SetIntrinsic(storage.IntrinsicIntlSegmentsPrototype, segmentsProto)
	iterProto := cx.NewObject(cx.Intrinsic(storage.IntrinsicIteratorPrototype))
	cx.SetIntrinsic(storage.IntrinsicIntlSegmentIteratorPrototype, iterProto)

	method(cx, ctor, "supportedLocalesOf", 1, nativeSupportedLocalesOf)
	method(cx, proto, "resolvedOptions", 0, nativeSegmenterResolvedOptions)
	method(cx, proto, "segment", 1, nativeSegmenterSegment)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Intl.Segmenter"), storage.FlagConfigurable)

	method(cx, segmentsProto, "containing", 1, nativeSegmentsContaining)
	cx.Define(segmentsProto, cx.WellKnownSymbol(storage.SymbolIterator), storage.ObjectValue(cx.NewNative("[Symbol.iterator]", 0, nativeSegmentsIterator)), storage.FlagsHidden)

	method(cx, iterProto, "next", 0, nativeSegmentIteratorNext)
	cx.Define(iterProto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Segmenter String Iterator"), storage.FlagConfigurable)
}

func segmenterResolvedOptions(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	sd, _ := storage.IntlData(this).(*segmenterData)
	requireSegmenter(cx, sd, "resolvedOptions")
	out := cx.NewObjectFromProperties(
		[]storage.PropKey{storage.NamedKey(cx.Intern("locale")), storage.NamedKey(cx.Intern("granularity"))},
		[]storage.Value{cx.NewString(wtf8.String(sd.locale)), cx.NewString(wtf8.String(sd.granularity))},
	)
	return storage.ObjectValue(out)
}

func segmenterSegment(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	sd, _ := storage.IntlData(this).(*segmenterData)
	requireSegmenter(cx, sd, "segment")
	s := cx.ToString(arg(args, 0))
	str := cx.NewString(s)
	return storage.ObjectValue(cx.NewIntlObject(cx.Intrinsic(storage.IntrinsicIntlSegmentsPrototype), newSegmentsData(cx, sd, str, s)))
}

func segmentsContaining(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	d, _ := storage.IntlData(this).(*segmentsData)
	requireSegments(cx, d, "containing")
	n := cx.ToIntegerOrInfinity(arg(args, 0))
	if n < 0 || n >= float64(d.length()) {
		return storage.Undefined
	}
	return d.segmentData(cx, d.segmentAt(cx, int(n)))
}

func segmentsIterator(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	d, _ := storage.IntlData(this).(*segmentsData)
	requireSegments(cx, d, "[Symbol.iterator]")
	return storage.ObjectValue(cx.NewIntlObject(cx.Intrinsic(storage.IntrinsicIntlSegmentIteratorPrototype), &segmentIteratorData{
		segments: d,
	}))
}

func segmentIteratorNext(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	it, ok := storage.IntlData(this).(*segmentIteratorData)
	if !ok {
		cx.ThrowTypeError("%SegmentIteratorPrototype%.next called on an object that is not a Segment Iterator")
	}
	d := it.segments
	if it.next+1 >= len(d.boundaries(cx)) {
		return cx.CreateIterResultObject(storage.Undefined, true)
	}
	v := d.segmentData(cx, it.next)
	it.next++
	return cx.CreateIterResultObject(v, false)
}
