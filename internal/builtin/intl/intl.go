// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package intl implements the Intl built-in objects of ECMA-402.
package intl

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// realm holds the Intl objects of a realm.
type realm struct {
	// ns is the Intl namespace, whose contents are created when they are first needed.
	ns *engine.Object

	localeProto *engine.Object
	localeCtor  *engine.Object

	numberFormatProto *engine.Object
	numberFormatCtor  *engine.Object
	pluralRulesProto  *engine.Object

	dateTimeFormatProto *engine.Object
	dateTimeFormatCtor  *engine.Object

	collatorProto  *engine.Object
	segmenterProto *engine.Object
	collatorCtor   *engine.Object
	// defaultCollator is the collator of String.prototype.localeCompare without locales and
	// options.
	defaultCollator *collatorData

	// fallbackSymbol is the [[FallbackSymbol]] of Intl.
	fallbackSymbol *engine.Object
}

// Install defines the Intl namespace in the current realm of vm.
func Install(vm *engine.VM) {
	r := &realm{}
	r.ns = vm.NewLazyObject(vm.Intrinsic(engine.IntrinsicObjectPrototype), func(ns *engine.Object) {
		r.init(vm)
	})
	vm.SetRealmExtension(r)
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Intl"), engine.ObjectValue(r.ns), engine.FlagsHidden)
	initToLocaleString(vm)
	initDateToLocaleString(vm)
	initTemporalToLocaleString(vm)
	initLocaleCompare(vm)
	initLocaleCase(vm)
	initArrayToLocaleString(vm)
}

// init defines the properties of the Intl namespace.
func (r *realm) init(vm *engine.VM) {
	ns := r.ns
	vm.Define(ns, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl"), engine.FlagConfigurable)
	method(vm, ns, "getCanonicalLocales", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return stringArray(vm, canonicalizeLocaleList(vm, arg(args, 0)))
	})
	method(vm, ns, "supportedValuesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var values []string
		switch vm.ToString(arg(args, 0)) {
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
			vm.ThrowRangeError("invalid key for Intl.supportedValuesOf")
		}
		return stringArray(vm, values)
	})
	r.fallbackSymbol = vm.NewSymbol("IntlLegacyConstructedSymbol", true)
	r.initLocale(vm)
	r.initNumberFormat(vm)
	r.initPluralRules(vm)
	r.initDateTimeFormat(vm)
	r.initCollator(vm)
	r.initSegmenter(vm)
}

// realmOf returns the Intl objects of the realm of the function f.
func realmOf(vm *engine.VM, f *engine.Object) *realm {
	r := vm.RealmExtension(vm.GetFunctionRealm(f)).(*realm)
	vm.InitLazyObject(r.ns)
	return r
}

// protoFor implements GetPrototypeFromConstructor for the constructor newTarget, with proto
// selecting the fallback prototype of a realm.
func protoFor(vm *engine.VM, newTarget *engine.Object, proto func(r *realm) *engine.Object) *engine.Object {
	if p := vm.GetV(engine.ObjectValue(newTarget), vm.Key(engine.KeyPrototype)); p.IsObject() {
		return p.Object()
	}
	return proto(realmOf(vm, newTarget))
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
func canonicalizeLocaleList(vm *engine.VM, locales engine.Value) []string {
	if locales.IsUndefined() {
		return nil
	}
	var o *engine.Object
	if _, ok := engine.IntlData(locales).(*localeData); ok || locales.IsString() {
		o = vm.NewArray([]engine.Value{locales})
	} else {
		o = vm.ToObject(locales)
	}
	n := vm.LengthOfArrayLike(o)
	var seen []string
	for k := range n {
		pk := vm.ToPropertyKey(engine.Number(float64(k)))
		if !vm.HasProperty(o, pk) {
			continue
		}
		v := vm.Get(o, pk)
		if !v.IsString() && !v.IsObject() {
			vm.ThrowTypeError("a locale must be a string or an object, not " + vm.DisplayString(v))
		}
		var tag string
		if l, ok := engine.IntlData(v).(*localeData); ok {
			tag = l.String()
		} else {
			tag = vm.ToString(v)
		}
		c, ok := locale.Canonicalize(tag)
		if !ok {
			vm.ThrowRangeError("invalid language tag: " + tag)
		}
		if !slices.Contains(seen, c) {
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

// method defines the non-enumerable function name on o.
func method(vm *engine.VM, o *engine.Object, name string, length int, f engine.NativeFunc) {
	vm.Define(o, vm.Intern(name), engine.ObjectValue(vm.NewNative(name, length, f)), engine.FlagsHidden)
}

// getter defines the accessor property name on o with the getter f.
func getter(vm *engine.VM, o *engine.Object, name string, f engine.NativeFunc) {
	vm.DefineAccessor(o, vm.Intern(name), vm.NewNative("get "+name, 0, f), nil, engine.FlagConfigurable)
}

// stringArray returns a new array of the strings ss.
func stringArray(vm *engine.VM, ss []string) engine.Value {
	vals := make([]engine.Value, len(ss))
	for i, s := range ss {
		vals[i] = vm.NewString(s)
	}
	return vm.NewArrayOf(vals)
}

// stringOrUndefined returns s as a String, or undefined when s is empty.
func stringOrUndefined(vm *engine.VM, s string, ok bool) engine.Value {
	if !ok {
		return engine.Undefined
	}
	return vm.NewString(s)
}

// getOption implements GetOption for an option of the type string without a default: it
// returns the option name of options converted to a String, and reports false when it is
// undefined. A non-empty values lists the allowed values.
func getOption(vm *engine.VM, options *engine.Object, name string, values ...string) (string, bool) {
	v := vm.Get(options, engine.NamedKey(vm.Intern(name)))
	if v.IsUndefined() {
		return "", false
	}
	s := vm.ToString(v)
	if len(values) > 0 && !slices.Contains(values, s) {
		vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for " + name)
	}
	return s, true
}
