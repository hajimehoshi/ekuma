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
func createDurationFormat(cx *engine.ExecutionContext, locales, opts engine.Value) *durationFormatData {
	requested := canonicalizeLocaleList(cx, locales)
	options := getOptionsObject(cx, opts, "Intl.DurationFormat")
	bestFit := localeMatcher(cx, options)
	nu, hasNu := numberingSystemOption(cx, options)
	res := locale.ResolveLocale(requested, defaultLocale(cx), bestFit, []locale.ExtensionKey{{
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
	o.Style = getStringOption(cx, options, "style", "short", "long", "short", "narrow", "digital")
	prevStyle := ""
	for i, unit := range duration.Units {
		o.Units[i] = getDurationUnitOptions(cx, unit, options, o.Style, durationStyles[i], prevStyle, o.TwoDigitHours)
		if i >= duration.Hours && i <= duration.Microseconds {
			prevStyle = o.Units[i].Style
		}
	}
	o.FractionalDigits = getNumberOption(cx, options, "fractionalDigits", 0, 9, -1)
	return df
}

// getDurationUnitOptions implements GetDurationUnitOptions.
func getDurationUnitOptions(cx *engine.ExecutionContext, unit string, options *engine.Object, baseStyle string, styles []string, prevStyle string, twoDigitHours bool) duration.UnitOptions {
	style, ok := getOption(cx, options, unit, styles...)
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
	display := getStringOption(cx, options, unit+"Display", displayDefault, "auto", "always")
	// ValidateDurationUnitStyle.
	switch {
	case display == "always" && style == "fractional":
		cx.ThrowRangeError(unit + " cannot be displayed always with a fractional style")
	case prevStyle == "fractional" && style != "fractional":
		cx.ThrowRangeError(unit + " must be fractional after a fractional unit")
	case (prevStyle == "numeric" || prevStyle == "2-digit") && style != "fractional" && style != "numeric" && style != "2-digit":
		cx.ThrowRangeError(unit + " must be numeric after a numeric unit")
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
func toDurationRecord(cx *engine.ExecutionContext, v engine.Value) duration.Duration {
	if d, ok := builtin.TemporalDurationOf(v); ok {
		return d.Fields()
	}
	if !v.IsObject() {
		if !v.IsString() {
			cx.ThrowTypeError("a duration must be an object or a string")
		}
		d, err := temporal.ParseTemporalDurationString(wtf8.ToValidUTF8(cx.ToString(v)), cx.Poll)
		if err != nil {
			cx.ThrowRangeError("invalid duration: " + cx.DisplayString(v))
		}
		if !temporal.IsValidDuration(&d) {
			cx.ThrowRangeError("duration is out of range")
		}
		return d.Fields()
	}
	o := v.Object()
	var d duration.Duration
	any := false
	// The fields in the alphabetical order of their names.
	for _, i := range []int{duration.Days, duration.Hours, duration.Microseconds, duration.Milliseconds, duration.Minutes, duration.Months, duration.Nanoseconds, duration.Seconds, duration.Weeks, duration.Years} {
		x := cx.Get(o, engine.NamedKey(cx.Intern(wtf8.String(duration.Units[i]))))
		if x.IsUndefined() {
			continue
		}
		any = true
		// ToIntegerIfIntegral.
		n := cx.ToNumber(x)
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			cx.ThrowRangeError(duration.Units[i] + " must be an integer")
		}
		d[i] = n + 0
	}
	if !any {
		cx.ThrowTypeError("a duration must have a duration field")
	}
	if !d.IsValid() {
		cx.ThrowRangeError("duration is out of range")
	}
	return d
}

// newDurationFormat implements the Intl.DurationFormat constructor.
func (r *realm) newDurationFormat(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.DurationFormat requires 'new'")
	}
	proto := protoFor(cx, nt, func(r *realm) *engine.Object { return r.durationFormatProto })
	return engine.ObjectValue(cx.NewIntlObject(proto, createDurationFormat(cx, arg(args, 0), arg(args, 1))))
}

// thisDurationFormat implements RequireInternalSlot for an Intl.DurationFormat.
func thisDurationFormat(cx *engine.ExecutionContext, this engine.Value, name string) *durationFormatData {
	df, ok := engine.IntlData(this).(*durationFormatData)
	if !ok {
		cx.ThrowTypeError("Intl.DurationFormat.prototype." + name + " called on an object that is not an Intl.DurationFormat")
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
func (r *realm) initDurationFormat(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := cx.NewNativeConstructor("DurationFormat", 0, r.newDurationFormat)
	cx.Define(ctor, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	cx.Define(r.ns, cx.Intern("DurationFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.durationFormatProto = proto

	method(cx, ctor, "supportedLocalesOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(cx, args)
	})
	method(cx, proto, "resolvedOptions", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		df := thisDurationFormat(cx, this, "resolvedOptions")
		o := &df.opts
		kv := []string{"locale", df.locale, "numberingSystem", o.NumberingSystem, "style", o.Style}
		for i, unit := range duration.Units {
			style := o.Units[i].Style
			if style == "fractional" {
				style = "numeric"
			}
			kv = append(kv, unit, style, unit+"Display", o.Units[i].Display)
		}
		out := resolvedOptions(cx, kv...)
		if o.FractionalDigits >= 0 {
			cx.CreateDataPropertyOrThrow(out.Object(), engine.NamedKey(cx.Intern("fractionalDigits")), engine.Number(float64(o.FractionalDigits)))
		}
		return out
	})
	method(cx, proto, "format", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		df := thisDurationFormat(cx, this, "format")
		return cx.NewString(wtf8.String(df.format(toDurationRecord(cx, arg(args, 0)))))
	})
	method(cx, proto, "formatToParts", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		df := thisDurationFormat(cx, this, "formatToParts")
		d := toDurationRecord(cx, arg(args, 0))
		parts := df.opts.Format(&d)
		return unitPartsArray(cx, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, parts[i].Unit
		})
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.DurationFormat"), engine.FlagConfigurable)
}

// durationToLocaleString implements Temporal.Duration.prototype.toLocaleString.
func durationToLocaleString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	d, ok := builtin.TemporalDurationOf(this)
	if !ok {
		cx.ThrowTypeError("Temporal.Duration.prototype.toLocaleString called on an object that is not a Temporal.Duration")
	}
	df := createDurationFormat(cx, arg(args, 0), arg(args, 1))
	return cx.NewString(wtf8.String(df.format(d.Fields())))
}
