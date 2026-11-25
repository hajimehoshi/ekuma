// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package intl implements the Intl built-in objects of ECMA-402.
package intl

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Install defines the Intl namespace in the realm of cx.
func Install(cx *engine.ExecutionContext) {
	ns := cx.NewLazyObject(cx.Intrinsic(storage.IntrinsicObjectPrototype), initNamespace)
	cx.SetIntrinsic(storage.IntrinsicIntl, ns)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Intl"), storage.ObjectValue(ns), storage.FlagsHidden)
	initToLocaleString(cx)
	initDateToLocaleString(cx)
	initTemporalToLocaleString(cx)
	initLocaleCompare(cx)
	initLocaleCase(cx)
	initArrayToLocaleString(cx)
}

var (
	nativeIntlGetCanonicalLocales = engine.RegisterNative(intlGetCanonicalLocales)
	nativeIntlSupportedValuesOf   = engine.RegisterNative(intlSupportedValuesOf)
)

// initNamespace defines the properties of the Intl namespace ns.
func initNamespace(cx *engine.ExecutionContext, ns *storage.Object) {
	cx.Define(ns, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Intl"), storage.FlagConfigurable)
	method(cx, ns, "getCanonicalLocales", 1, nativeIntlGetCanonicalLocales)
	method(cx, ns, "supportedValuesOf", 1, nativeIntlSupportedValuesOf)
	initLocale(cx, ns)
	initNumberFormat(cx, ns)
	initPluralRules(cx, ns)
	initDateTimeFormat(cx, ns)
	initCollator(cx, ns)
	initSegmenter(cx, ns)
	initListFormat(cx, ns)
	initRelativeTimeFormat(cx, ns)
	initDisplayNames(cx, ns)
	initDurationFormat(cx, ns)
}

func intlGetCanonicalLocales(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return stringArray(cx, canonicalizeLocaleList(cx, arg(args, 0)))
}

func intlSupportedValuesOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	var values []string
	switch cx.ToString(arg(args, 0)) {
	case "calendar":
		values = locale.Calendars
	case "collation":
		values = locale.Collations()
	case "currency":
		values = make([]string, cldr.Currencies.Len())
		for i := range values {
			values[i] = cldr.Currencies.At(i)
		}
	case "numberingSystem":
		values = locale.NumberingSystems()
	case "timeZone":
		values = locale.TimeZones()
	case "unit":
		values = sanctionedUnits
	default:
		cx.ThrowRangeError("invalid key for Intl.supportedValuesOf")
	}
	return stringArray(cx, values)
}

// sanctionedUnits are the sanctioned single unit identifiers of ECMA-402, sorted.
var sanctionedUnits = []string{
	"acre", "bit", "byte", "celsius", "centimeter", "day", "degree", "fahrenheit", "fluid-ounce",
	"foot", "gallon", "gigabit", "gigabyte", "gram", "hectare", "hour", "inch", "kilobit",
	"kilobyte", "kilogram", "kilometer", "liter", "megabit", "megabyte", "meter", "microsecond",
	"mile", "mile-scandinavian", "milliliter", "millimeter", "millisecond", "minute", "month",
	"nanosecond", "ounce", "percent", "petabyte", "pound", "second", "stone", "terabit",
	"terabyte", "week", "yard", "year",
}

// canonicalizeLocaleList implements CanonicalizeLocaleList.
func canonicalizeLocaleList(cx *engine.ExecutionContext, locales storage.Value) []string {
	if locales.IsUndefined() {
		return nil
	}
	var o *storage.Object
	if _, ok := storage.IntlData(locales).(*localeData); ok || locales.IsString() {
		o = cx.NewArray([]storage.Value{locales})
	} else {
		o = cx.ToObject(locales)
	}
	n := cx.LengthOfArrayLike(o)
	var seen []string
	var seenSet map[string]struct{}
	for k := range n {
		cx.PollStep()
		pk := cx.ToPropertyKey(storage.Number(float64(k)))
		if !cx.HasProperty(o, pk) {
			continue
		}
		v := cx.Get(o, pk)
		if !v.IsString() && !v.IsObject() {
			cx.ThrowTypeError("a locale must be a string or an object, not " + cx.DisplayString(v))
		}
		var tag string
		if l, ok := storage.IntlData(v).(*localeData); ok {
			tag = l.String()
		} else {
			tag = wtf8.ToValidUTF8(cx.ToString(v))
		}
		c, ok := locale.Canonicalize(tag, cx.Poll)
		if !ok {
			cx.ThrowRangeError("invalid language tag: " + cx.DisplayString(cx.NewString(wtf8.String(tag))))
		}
		if !localetag.HasDuplicate(&seenSet, seen, c) {
			seen = append(seen, c)
		}
	}
	return seen
}

// arg returns the argument i, or undefined when it was not passed.
func arg(args []storage.Value, i int) storage.Value {
	if i < len(args) {
		return args[i]
	}
	return storage.Undefined
}

// checkStringLength throws a RangeError when n is more bytes than a string may have.
func checkStringLength(cx *engine.ExecutionContext, n int) {
	if n > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
}

// constructor creates the constructor function name with the intrinsic prototype protoID, and
// defines it on the Intl namespace ns.
func constructor(cx *engine.ExecutionContext, ns *storage.Object, name wtf8.String, length int, protoID storage.Intrinsic, f storage.Native) (ctor, proto *storage.Object) {
	proto = cx.NewObject(cx.Intrinsic(storage.IntrinsicObjectPrototype))
	cx.SetIntrinsic(protoID, proto)
	ctor = cx.NewNativeConstructor(name, length, f)
	cx.DefineConstructorPrototype(ctor, protoID)
	cx.Define(proto, cx.Key(storage.KeyConstructor), storage.ObjectValue(ctor), storage.FlagsHidden)
	cx.Define(ns, cx.Intern(name), storage.ObjectValue(ctor), storage.FlagsHidden)
	return ctor, proto
}

// method defines the non-enumerable function name on o.
func method(cx *engine.ExecutionContext, o *storage.Object, name string, length int, f storage.Native) {
	cx.Define(o, cx.Intern(wtf8.String(name)), storage.ObjectValue(cx.NewNative(wtf8.String(name), length, f)), storage.FlagsHidden)
}

// getter defines the accessor property name on o with the getter f.
func getter(cx *engine.ExecutionContext, o *storage.Object, name string, f storage.Native) {
	cx.DefineAccessor(o, cx.Intern(wtf8.String(name)), cx.NewNative(wtf8.String("get "+name), 0, f), nil, storage.FlagConfigurable)
}

// stringArray returns a new array of the strings ss.
func stringArray(cx *engine.ExecutionContext, ss []string) storage.Value {
	vals := make([]storage.Value, len(ss))
	for i, s := range ss {
		vals[i] = cx.NewString(wtf8.String(s))
	}
	return cx.NewArrayOf(vals)
}

// stringOrUndefined returns s as a String when ok is true, or undefined otherwise.
func stringOrUndefined(cx *engine.ExecutionContext, s string, ok bool) storage.Value {
	if !ok {
		return storage.Undefined
	}
	return cx.NewString(wtf8.String(s))
}

// getOption implements GetOption for an option of the type string without a default: it
// returns the option name of options converted to a String, and reports false when it is
// undefined. A non-empty values lists the allowed values.
func getOption(cx *engine.ExecutionContext, options *storage.Object, name string, values ...string) (string, bool) {
	v := cx.Get(options, storage.NamedKey(cx.Intern(wtf8.String(name))))
	if v.IsUndefined() {
		return "", false
	}
	s := wtf8.ToValidUTF8(cx.ToString(v))
	if len(values) > 0 && !slices.Contains(values, s) {
		cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for " + name)
	}
	return s, true
}

// defaultLocale implements DefaultLocale.
func defaultLocale(cx *engine.ExecutionContext) string {
	if l := cx.AvailableDefaultLocale(); l != "" {
		return l
	}
	var l string
	if t, ok := locale.Parse(cx.DefaultLocale()); ok {
		l, _ = locale.BestAvailableLocale(t.Canonicalize().WithoutUnicode().String())
	}
	if l == "" {
		l = locale.Default()
	}
	cx.SetAvailableDefaultLocale(l)
	return l
}
