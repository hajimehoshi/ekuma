// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package intl implements the Intl built-in objects of ECMA-402.
package intl

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// realm holds the Intl objects of a realm.
type realm struct {
	// ns is the Intl namespace, whose contents are created when they are first needed.
	ns *engine.Object

	defaultLocaleTag string
	defaultLocale    string

	localeProto *engine.Object
	localeCtor  *engine.Object

	numberFormatProto *engine.Object
	numberFormatCtor  *engine.Object
	pluralRulesProto  *engine.Object

	dateTimeFormatProto *engine.Object
	dateTimeFormatCtor  *engine.Object

	collatorProto        *engine.Object
	segmenterProto       *engine.Object
	segmentsProto        *engine.Object
	segmentIteratorProto *engine.Object
	collatorCtor         *engine.Object
	// defaultCollator is the collator of String.prototype.localeCompare without locales and
	// options.
	defaultCollator       *collatorData
	defaultCollatorLocale string

	listFormatProto         *engine.Object
	relativeTimeFormatProto *engine.Object
	displayNamesProto       *engine.Object
	durationFormatProto     *engine.Object

	// fallbackSymbol is the [[FallbackSymbol]] of Intl.
	fallbackSymbol *engine.Object
}

// Install defines the Intl namespace in the realm of cx.
func Install(cx *engine.ExecutionContext) {
	r := &realm{}
	r.ns = cx.NewLazyObject(cx.Intrinsic(engine.IntrinsicObjectPrototype), func(cx *engine.ExecutionContext, ns *engine.Object) {
		r.init(cx)
	})
	cx.SetRealmExtension(r)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Intl"), engine.ObjectValue(r.ns), engine.FlagsHidden)
	initToLocaleString(cx)
	initDateToLocaleString(cx)
	initTemporalToLocaleString(cx)
	initLocaleCompare(cx)
	initLocaleCase(cx)
	initArrayToLocaleString(cx)
}

// init defines the properties of the Intl namespace.
func (r *realm) init(cx *engine.ExecutionContext) {
	ns := r.ns
	cx.Define(ns, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl"), engine.FlagConfigurable)
	method(cx, ns, "getCanonicalLocales", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return stringArray(cx, canonicalizeLocaleList(cx, arg(args, 0)))
	})
	method(cx, ns, "supportedValuesOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})
	r.fallbackSymbol = cx.NewSymbol("IntlLegacyConstructedSymbol", true)
	r.initLocale(cx)
	r.initNumberFormat(cx)
	r.initPluralRules(cx)
	r.initDateTimeFormat(cx)
	r.initCollator(cx)
	r.initSegmenter(cx)
	r.initListFormat(cx)
	r.initRelativeTimeFormat(cx)
	r.initDisplayNames(cx)
	r.initDurationFormat(cx)
}

// realmOf returns the Intl objects of the realm of the function f.
func realmOf(cx *engine.ExecutionContext, f *engine.Object) *realm {
	r := cx.FunctionContext(f).RealmExtension().(*realm)
	cx.InitLazyObject(r.ns)
	return r
}

// protoFor implements GetPrototypeFromConstructor for the constructor newTarget, with proto
// selecting the fallback prototype of a realm.
func protoFor(cx *engine.ExecutionContext, newTarget *engine.Object, proto func(r *realm) *engine.Object) *engine.Object {
	if p := cx.GetV(engine.ObjectValue(newTarget), cx.Key(engine.KeyPrototype)); p.IsObject() {
		return p.Object()
	}
	return proto(realmOf(cx, newTarget))
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
func canonicalizeLocaleList(cx *engine.ExecutionContext, locales engine.Value) []string {
	if locales.IsUndefined() {
		return nil
	}
	var o *engine.Object
	if _, ok := engine.IntlData(locales).(*localeData); ok || locales.IsString() {
		o = cx.NewArray([]engine.Value{locales})
	} else {
		o = cx.ToObject(locales)
	}
	n := cx.LengthOfArrayLike(o)
	var seen []string
	var seenSet map[string]struct{}
	for k := range n {
		cx.PollAt(k)
		pk := cx.ToPropertyKey(engine.Number(float64(k)))
		if !cx.HasProperty(o, pk) {
			continue
		}
		v := cx.Get(o, pk)
		if !v.IsString() && !v.IsObject() {
			cx.ThrowTypeError("a locale must be a string or an object, not " + cx.DisplayString(v))
		}
		var tag string
		if l, ok := engine.IntlData(v).(*localeData); ok {
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
func arg(args []engine.Value, i int) engine.Value {
	if i < len(args) {
		return args[i]
	}
	return engine.Undefined
}

// checkStringLength throws a RangeError when n is more bytes than a string may have.
func checkStringLength(cx *engine.ExecutionContext, n int) {
	if n > engine.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
}

// method defines the non-enumerable function name on o.
func method(cx *engine.ExecutionContext, o *engine.Object, name string, length int, f engine.NativeFunc) {
	cx.Define(o, cx.Intern(wtf8.String(name)), engine.ObjectValue(cx.NewNative(wtf8.String(name), length, f)), engine.FlagsHidden)
}

// getter defines the accessor property name on o with the getter f.
func getter(cx *engine.ExecutionContext, o *engine.Object, name string, f engine.NativeFunc) {
	cx.DefineAccessor(o, cx.Intern(wtf8.String(name)), cx.NewNative(wtf8.String("get "+name), 0, f), nil, engine.FlagConfigurable)
}

// stringArray returns a new array of the strings ss.
func stringArray(cx *engine.ExecutionContext, ss []string) engine.Value {
	vals := make([]engine.Value, len(ss))
	for i, s := range ss {
		vals[i] = cx.NewString(wtf8.String(s))
	}
	return cx.NewArrayOf(vals)
}

// stringOrUndefined returns s as a String when ok is true, or undefined otherwise.
func stringOrUndefined(cx *engine.ExecutionContext, s string, ok bool) engine.Value {
	if !ok {
		return engine.Undefined
	}
	return cx.NewString(wtf8.String(s))
}

// getOption implements GetOption for an option of the type string without a default: it
// returns the option name of options converted to a String, and reports false when it is
// undefined. A non-empty values lists the allowed values.
func getOption(cx *engine.ExecutionContext, options *engine.Object, name string, values ...string) (string, bool) {
	v := cx.Get(options, engine.NamedKey(cx.Intern(wtf8.String(name))))
	if v.IsUndefined() {
		return "", false
	}
	s := wtf8.ToValidUTF8(cx.ToString(v))
	if len(values) > 0 && !slices.Contains(values, s) {
		cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for " + name)
	}
	return s, true
}

func defaultLocale(cx *engine.ExecutionContext) string {
	r := cx.RealmExtension().(*realm)
	tag := cx.DefaultLocale()
	if r.defaultLocale != "" && r.defaultLocaleTag == tag {
		return r.defaultLocale
	}
	var resolved string
	if t, ok := locale.Parse(tag); ok {
		resolved, _ = locale.BestAvailableLocale(t.Canonicalize().WithoutUnicode().String())
	}
	if resolved == "" {
		resolved = locale.Default()
	}
	r.defaultLocaleTag = tag
	r.defaultLocale = resolved
	return resolved
}
