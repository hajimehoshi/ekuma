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
func (r *realm) newRelativeTimeFormat(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError("constructor Intl.RelativeTimeFormat requires 'new'")
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.relativeTimeFormatProto })
	requested := canonicalizeLocaleList(vm, arg(args, 0))
	options := optionsObject(vm, arg(args, 1))
	bestFit := localeMatcher(vm, options)
	nu, hasNu := numberingSystemOption(vm, options)
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, []locale.ExtensionKey{{
		Key:       "nu",
		Option:    nu,
		HasOption: hasNu,
		Values:    numberingSystemsOf,
	}})
	rtf := &relativeTimeFormatData{
		locale:          res.Locale,
		dataLocale:      res.DataLocale,
		numberingSystem: res.Values[0],
		style:           getStringOption(vm, options, "style", "long", "long", "short", "narrow"),
		numeric:         getStringOption(vm, options, "numeric", "always", "always", "auto"),
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
	return engine.ObjectValue(vm.NewIntlObject(proto, rtf))
}

// thisRelativeTimeFormat implements RequireInternalSlot for an Intl.RelativeTimeFormat.
func thisRelativeTimeFormat(vm *engine.VM, this engine.Value, name string) *relativeTimeFormatData {
	rtf, ok := engine.IntlData(this).(*relativeTimeFormatData)
	if !ok {
		vm.ThrowTypeError("Intl.RelativeTimeFormat.prototype." + name + " called on an object that is not an Intl.RelativeTimeFormat")
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
func (rtf *relativeTimeFormatData) partition(vm *engine.VM, value float64, unit string) []relativeTimePart {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		vm.ThrowRangeError("the value of a relative time must be finite")
	}
	// SingularRelativeTimeUnit.
	switch unit {
	case "seconds", "minutes", "hours", "days", "weeks", "months", "quarters", "years":
		unit = strings.TrimSuffix(unit, "s")
	case "second", "minute", "hour", "day", "week", "month", "quarter", "year":
	default:
		vm.ThrowRangeError("invalid unit of a relative time: " + unit)
	}
	patterns := reltime.PatternsOf(rtf.dataLocale, unit, rtf.style)
	if rtf.numeric == "auto" {
		if s, ok := patterns.Named(vm.ToString(engine.Number(value))); ok {
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
func (rtf *relativeTimeFormatData) partitionArgs(vm *engine.VM, args []engine.Value) []relativeTimePart {
	value := vm.ToNumber(arg(args, 0))
	unit := vm.ToString(arg(args, 1))
	return rtf.partition(vm, value, unit)
}

// initRelativeTimeFormat defines Intl.RelativeTimeFormat.
func (r *realm) initRelativeTimeFormat(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("RelativeTimeFormat", 0, r.newRelativeTimeFormat)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("RelativeTimeFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.relativeTimeFormatProto = proto

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	method(vm, proto, "format", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		rtf := thisRelativeTimeFormat(vm, this, "format")
		var b strings.Builder
		for _, p := range rtf.partitionArgs(vm, args) {
			b.WriteString(p.value)
		}
		return vm.NewString(b.String())
	})
	method(vm, proto, "formatToParts", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		rtf := thisRelativeTimeFormat(vm, this, "formatToParts")
		parts := rtf.partitionArgs(vm, args)
		return unitPartsArray(vm, len(parts), func(i int) (string, string, string) {
			return parts[i].typ, parts[i].value, parts[i].unit
		})
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		rtf := thisRelativeTimeFormat(vm, this, "resolvedOptions")
		return resolvedOptions(vm, "locale", rtf.locale, "style", rtf.style, "numeric", rtf.numeric, "numberingSystem", rtf.numberingSystem)
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.RelativeTimeFormat"), engine.FlagConfigurable)
}

// unitPartsArray returns the parts as an array of objects with the properties "type" and
// "value", and "unit" for the parts with a unit.
func unitPartsArray(vm *engine.VM, n int, part func(i int) (typ, value, unit string)) engine.Value {
	keys := [...]engine.PropKey{
		engine.NamedKey(vm.Intern("type")),
		engine.NamedKey(vm.Intern("value")),
		engine.NamedKey(vm.Intern("unit")),
	}
	vals := make([]engine.Value, n)
	for i := range n {
		typ, value, unit := part(i)
		props := [3]engine.Value{vm.NewString(typ), vm.NewString(value)}
		propertyCount := 2
		if unit != "" {
			props[2] = vm.NewString(unit)
			propertyCount++
		}
		vals[i] = engine.ObjectValue(vm.NewObjectFromProperties(keys[:propertyCount], props[:propertyCount]))
	}
	return vm.NewArrayOf(vals)
}
