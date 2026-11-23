// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/duration"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// durationFormatData holds the internal slots of an Intl.DurationFormat object.
type durationFormatData struct {
	locale string
	opts   duration.Options
}

// durationStyles are the styles of the units of a duration, indexed like [duration.Units].
var durationStyles = [len(duration.Units)][]string{
	{"long", "short", "narrow"},
	{"long", "short", "narrow"},
	{"long", "short", "narrow"},
	{"long", "short", "narrow"},
	{"long", "short", "narrow", "numeric", "2-digit"},
	{"long", "short", "narrow", "numeric", "2-digit"},
	{"long", "short", "narrow", "numeric", "2-digit"},
	{"long", "short", "narrow", "numeric"},
	{"long", "short", "narrow", "numeric"},
	{"long", "short", "narrow", "numeric"},
}

// createDurationFormat implements the steps of the Intl.DurationFormat constructor that
// resolve the internal slots.
func createDurationFormat(vm *engine.VM, locales, opts engine.Value) *durationFormatData {
	requested := canonicalizeLocaleList(vm, locales)
	options := getOptionsObject(vm, opts, "Intl.DurationFormat")
	bestFit := localeMatcher(vm, options)
	nu, hasNu := numberingSystemOption(vm, options)
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, []locale.ExtensionKey{{
		Key:       "nu",
		Option:    nu,
		HasOption: hasNu,
		Values:    numberingSystemsOf,
	}})
	df := &durationFormatData{
		locale: res.Locale,
	}
	o := &df.opts
	o.Locale = res.DataLocale
	o.DigitalFormat = duration.DigitalFormatOf(res.DataLocale)
	o.NumberingSystem = res.Values[0]
	o.Style = getStringOption(vm, options, "style", "short", "long", "short", "narrow", "digital")
	prevStyle := ""
	for i, unit := range duration.Units {
		o.Units[i] = getDurationUnitOptions(vm, unit, options, o.Style, durationStyles[i], prevStyle, o.TwoDigitHours)
		if i >= duration.Hours && i <= duration.Microseconds {
			prevStyle = o.Units[i].Style
		}
	}
	o.FractionalDigits = getNumberOption(vm, options, "fractionalDigits", 0, 9, -1)
	return df
}

// getDurationUnitOptions implements GetDurationUnitOptions.
func getDurationUnitOptions(vm *engine.VM, unit string, options *engine.Object, baseStyle string, styles []string, prevStyle string, twoDigitHours bool) duration.UnitOptions {
	style, ok := getOption(vm, options, unit, styles...)
	displayDefault := "always"
	fractional := unit == "milliseconds" || unit == "microseconds" || unit == "nanoseconds"
	if !ok {
		switch {
		case baseStyle == "digital":
			if len(styles) == 3 {
				style = "short"
			} else {
				style = "numeric"
			}
			if unit != "hours" && unit != "minutes" && unit != "seconds" {
				displayDefault = "auto"
			}
		case prevStyle == "fractional" || prevStyle == "numeric" || prevStyle == "2-digit":
			style = "numeric"
			if unit != "minutes" && unit != "seconds" {
				displayDefault = "auto"
			}
		default:
			style = baseStyle
			displayDefault = "auto"
		}
	}
	if style == "numeric" && fractional {
		style = "fractional"
		displayDefault = "auto"
	}
	display := getStringOption(vm, options, unit+"Display", displayDefault, "auto", "always")
	// ValidateDurationUnitStyle.
	switch {
	case display == "always" && style == "fractional":
		vm.ThrowRangeError(vm.CurrentRealm(), unit+" cannot be displayed always with a fractional style")
	case prevStyle == "fractional" && style != "fractional":
		vm.ThrowRangeError(vm.CurrentRealm(), unit+" must be fractional after a fractional unit")
	case (prevStyle == "numeric" || prevStyle == "2-digit") && style != "fractional" && style != "numeric" && style != "2-digit":
		vm.ThrowRangeError(vm.CurrentRealm(), unit+" must be numeric after a numeric unit")
	}
	if unit == "hours" && twoDigitHours {
		style = "2-digit"
	}
	if (unit == "minutes" || unit == "seconds") && (prevStyle == "numeric" || prevStyle == "2-digit") {
		style = "2-digit"
	}
	return duration.UnitOptions{
		Style:   style,
		Display: display,
	}
}

// toDurationRecord implements ToDurationRecord, with the Temporal.Duration objects and the
// strings of Temporal.
func toDurationRecord(vm *engine.VM, v engine.Value) duration.Duration {
	if d, ok := builtin.TemporalDurationOf(v); ok {
		return d.Fields()
	}
	if !v.IsObject() {
		if !v.IsString() {
			vm.ThrowTypeError(vm.CurrentRealm(), "a duration must be an object or a string")
		}
		d, err := temporal.ParseTemporalDurationString(wtf8.ToValidUTF8(vm.ToString(v)), vm.Poll)
		if err != nil {
			vm.ThrowRangeError(vm.CurrentRealm(), "invalid duration: "+vm.DisplayString(v))
		}
		if !temporal.IsValidDuration(&d) {
			vm.ThrowRangeError(vm.CurrentRealm(), "duration is out of range")
		}
		return d.Fields()
	}
	o := v.Object()
	var d duration.Duration
	any := false
	// The fields in the alphabetical order of their names.
	for _, i := range []int{duration.Days, duration.Hours, duration.Microseconds, duration.Milliseconds, duration.Minutes, duration.Months, duration.Nanoseconds, duration.Seconds, duration.Weeks, duration.Years} {
		x := vm.Get(o, engine.NamedKey(vm.Intern(wtf8.String(duration.Units[i]))))
		if x.IsUndefined() {
			continue
		}
		any = true
		// ToIntegerIfIntegral.
		n := vm.ToNumber(x)
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			vm.ThrowRangeError(vm.CurrentRealm(), duration.Units[i]+" must be an integer")
		}
		d[i] = n + 0
	}
	if !any {
		vm.ThrowTypeError(vm.CurrentRealm(), "a duration must have a duration field")
	}
	if !d.IsValid() {
		vm.ThrowRangeError(vm.CurrentRealm(), "duration is out of range")
	}
	return d
}

// newDurationFormat implements the Intl.DurationFormat constructor.
func (r *realm) newDurationFormat(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError(vm.CurrentRealm(), "constructor Intl.DurationFormat requires 'new'")
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.durationFormatProto })
	return engine.ObjectValue(vm.NewIntlObject(proto, createDurationFormat(vm, arg(args, 0), arg(args, 1))))
}

// thisDurationFormat implements RequireInternalSlot for an Intl.DurationFormat.
func thisDurationFormat(vm *engine.VM, this engine.Value, name string) *durationFormatData {
	df, ok := engine.IntlData(this).(*durationFormatData)
	if !ok {
		vm.ThrowTypeError(vm.CurrentRealm(), "Intl.DurationFormat.prototype."+name+" called on an object that is not an Intl.DurationFormat")
	}
	return df
}

// format returns the formatted duration d.
func (df *durationFormatData) format(d duration.Duration) string {
	var b strings.Builder
	for _, p := range df.opts.Format(&d) {
		b.WriteString(p.Value)
	}
	return b.String()
}

// initDurationFormat defines Intl.DurationFormat.
func (r *realm) initDurationFormat(vm *engine.VM) {
	proto := vm.NewObject(vm.CurrentRealm().Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("DurationFormat", 0, r.newDurationFormat)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("DurationFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.durationFormatProto = proto

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		df := thisDurationFormat(vm, this, "resolvedOptions")
		o := &df.opts
		kv := []string{"locale", df.locale, "numberingSystem", o.NumberingSystem, "style", o.Style}
		for i, unit := range duration.Units {
			style := o.Units[i].Style
			if style == "fractional" {
				style = "numeric"
			}
			kv = append(kv, unit, style, unit+"Display", o.Units[i].Display)
		}
		out := resolvedOptions(vm, kv...)
		if o.FractionalDigits >= 0 {
			vm.CreateDataPropertyOrThrow(out.Object(), engine.NamedKey(vm.Intern("fractionalDigits")), engine.Number(float64(o.FractionalDigits)))
		}
		return out
	})
	method(vm, proto, "format", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		df := thisDurationFormat(vm, this, "format")
		return vm.NewString(wtf8.String(df.format(toDurationRecord(vm, arg(args, 0)))))
	})
	method(vm, proto, "formatToParts", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		df := thisDurationFormat(vm, this, "formatToParts")
		d := toDurationRecord(vm, arg(args, 0))
		parts := df.opts.Format(&d)
		return unitPartsArray(vm, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, parts[i].Unit
		})
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.DurationFormat"), engine.FlagConfigurable)
}

// durationToLocaleString implements Temporal.Duration.prototype.toLocaleString.
func durationToLocaleString(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	d, ok := builtin.TemporalDurationOf(this)
	if !ok {
		vm.ThrowTypeError(vm.CurrentRealm(), "Temporal.Duration.prototype.toLocaleString called on an object that is not a Temporal.Duration")
	}
	df := createDurationFormat(vm, arg(args, 0), arg(args, 1))
	return vm.NewString(wtf8.String(df.format(d.Fields())))
}
