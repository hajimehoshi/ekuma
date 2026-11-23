// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/intl/number"
	"github.com/hajimehoshi/ekuma/internal/intl/plural"
	"github.com/hajimehoshi/ekuma/internal/intl/reltime"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// relativeTimeFormatData holds the internal slots of an Intl.RelativeTimeFormat object.
type relativeTimeFormatData struct {
	locale string
	// dataLocale is the available locale whose data is used.
	dataLocale      string
	numberingSystem string
	style           string
	numeric         string
	nf              *number.Formatter
	// pluralDigits are the digit options of [[PluralRules]].
	pluralDigits number.Digits
}

// defaultDigits returns the digit options of a PluralRules, or of a decimal NumberFormat in
// standard notation, constructed without digit options.
func defaultDigits() number.Digits {
	return number.Digits{
		MinimumIntegerDigits:  1,
		MaximumFractionDigits: 3,
		RoundingType:          number.FractionDigits,
		RoundingIncrement:     1,
		RoundingMode:          "halfExpand",
	}
}

// newRelativeTimeFormat implements the Intl.RelativeTimeFormat constructor.
func (r *realm) newRelativeTimeFormat(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.RelativeTimeFormat requires 'new'")
	}
	proto := protoFor(cx, nt, func(r *realm) *engine.Object { return r.relativeTimeFormatProto })
	requested := canonicalizeLocaleList(cx, arg(args, 0))
	options := optionsObject(cx, arg(args, 1))
	bestFit := localeMatcher(cx, options)
	nu, hasNu := numberingSystemOption(cx, options)
	res := locale.ResolveLocale(requested, defaultLocale(cx), bestFit, []locale.ExtensionKey{{
		Key:       "nu",
		Option:    nu,
		HasOption: hasNu,
		Values:    numberingSystemsOf,
	}})
	rtf := &relativeTimeFormatData{
		locale:          res.Locale,
		dataLocale:      res.DataLocale,
		numberingSystem: res.Values[0],
		style:           getStringOption(cx, options, "style", "long", "long", "short", "narrow"),
		numeric:         getStringOption(cx, options, "numeric", "always", "always", "auto"),
		pluralDigits:    defaultDigits(),
	}
	rtf.nf = number.NewFormatter(&number.Options{
		Locale:          res.DataLocale,
		NumberingSystem: rtf.numberingSystem,
		Style:           "decimal",
		Notation:        "standard",
		UseGrouping:     "auto",
		SignDisplay:     "auto",
		Digits:          defaultDigits(),
	})
	return engine.ObjectValue(cx.NewIntlObject(proto, rtf))
}

// thisRelativeTimeFormat implements RequireInternalSlot for an Intl.RelativeTimeFormat.
func thisRelativeTimeFormat(cx *engine.ExecutionContext, this engine.Value, name string) *relativeTimeFormatData {
	rtf, ok := engine.IntlData(this).(*relativeTimeFormatData)
	if !ok {
		cx.ThrowTypeError("Intl.RelativeTimeFormat.prototype." + name + " called on an object that is not an Intl.RelativeTimeFormat")
	}
	return rtf
}

// relativeTimePart is a part of a formatted relative time.
type relativeTimePart struct {
	typ, value string
	// unit is the unit of a part of the number, or empty.
	unit string
}

// partition implements PartitionRelativeTimePattern after the conversion of the arguments.
func (rtf *relativeTimeFormatData) partition(cx *engine.ExecutionContext, value float64, unit string) []relativeTimePart {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		cx.ThrowRangeError("the value of a relative time must be finite")
	}
	// SingularRelativeTimeUnit.
	switch unit {
	case "seconds", "minutes", "hours", "days", "weeks", "months", "quarters", "years":
		unit = strings.TrimSuffix(unit, "s")
	case "second", "minute", "hour", "day", "week", "month", "quarter", "year":
	default:
		cx.ThrowRangeError("invalid unit of a relative time: " + cx.DisplayString(cx.NewString(wtf8.String(unit))))
	}
	patterns := reltime.PatternsOf(rtf.dataLocale, unit, rtf.style)
	if rtf.numeric == "auto" {
		if s, ok := patterns.Named(string(cx.ToString(engine.Number(value)))); ok {
			return []relativeTimePart{{typ: "literal", value: s}}
		}
	}
	past := math.Signbit(value)
	x := number.FromFloat(math.Abs(value))
	s, _ := rtf.pluralDigits.FormatToString(x)
	category := plural.Select(locale.DataLocale(rtf.dataLocale), false, s, 0)
	pattern := patterns.Pattern(past, category)
	formatted := rtf.nf.Format(x, nil)
	var out []relativeTimePart
	for pattern != "" {
		before, after, ok := strings.Cut(pattern, "{0}")
		if before != "" {
			out = append(out, relativeTimePart{typ: "literal", value: before})
		}
		if !ok {
			break
		}
		for _, p := range formatted {
			out = append(out, relativeTimePart{typ: p.Type, value: p.Value, unit: unit})
		}
		pattern = after
	}
	return out
}

// partitionArgs implements PartitionRelativeTimePattern for the arguments of format and
// formatToParts, converting them first.
func (rtf *relativeTimeFormatData) partitionArgs(cx *engine.ExecutionContext, args []engine.Value) []relativeTimePart {
	value := cx.ToNumber(arg(args, 0))
	unit := wtf8.ToValidUTF8(cx.ToString(arg(args, 1)))
	return rtf.partition(cx, value, unit)
}

// initRelativeTimeFormat defines Intl.RelativeTimeFormat.
func (r *realm) initRelativeTimeFormat(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := cx.NewNativeConstructor("RelativeTimeFormat", 0, r.newRelativeTimeFormat)
	cx.Define(ctor, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	cx.Define(r.ns, cx.Intern("RelativeTimeFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.relativeTimeFormatProto = proto

	method(cx, ctor, "supportedLocalesOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(cx, args)
	})
	method(cx, proto, "format", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		rtf := thisRelativeTimeFormat(cx, this, "format")
		var b strings.Builder
		for _, p := range rtf.partitionArgs(cx, args) {
			b.WriteString(p.value)
		}
		return cx.NewString(wtf8.String(b.String()))
	})
	method(cx, proto, "formatToParts", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		rtf := thisRelativeTimeFormat(cx, this, "formatToParts")
		parts := rtf.partitionArgs(cx, args)
		return unitPartsArray(cx, len(parts), func(i int) (string, string, string) {
			return parts[i].typ, parts[i].value, parts[i].unit
		})
	})
	method(cx, proto, "resolvedOptions", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		rtf := thisRelativeTimeFormat(cx, this, "resolvedOptions")
		return resolvedOptions(cx, "locale", rtf.locale, "style", rtf.style, "numeric", rtf.numeric, "numberingSystem", rtf.numberingSystem)
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.RelativeTimeFormat"), engine.FlagConfigurable)
}

// unitPartsArray returns the parts as an array of objects with the properties "type" and
// "value", and "unit" for the parts with a unit.
func unitPartsArray(cx *engine.ExecutionContext, n int, part func(i int) (typ, value, unit string)) engine.Value {
	keys := [...]engine.PropKey{
		engine.NamedKey(cx.Intern("type")),
		engine.NamedKey(cx.Intern("value")),
		engine.NamedKey(cx.Intern("unit")),
	}
	vals := make([]engine.Value, n)
	for i := range n {
		typ, value, unit := part(i)
		props := [3]engine.Value{cx.NewString(wtf8.String(typ)), cx.NewString(wtf8.String(value))}
		propertyCount := 2
		if unit != "" {
			props[2] = cx.NewString(wtf8.String(unit))
			propertyCount++
		}
		vals[i] = engine.ObjectValue(cx.NewObjectFromProperties(keys[:propertyCount], props[:propertyCount]))
	}
	return cx.NewArrayOf(vals)
}
