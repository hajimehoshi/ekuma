// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/intl/number"
	"github.com/hajimehoshi/ekuma/internal/intl/plural"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// pluralRulesData holds the internal slots of an Intl.PluralRules object.
type pluralRulesData struct {
	locale string
	// dataLocale is the locale of CLDR whose rules apply.
	dataLocale     string
	ordinal        bool
	notation       string
	compactDisplay string
	digits         number.Digits
	slots          digitSlots
}

// resolvePlural implements ResolvePlural for n. It returns the plural category and the
// formatted string.
func (pr *pluralRulesData) resolvePlural(n number.Decimal) (string, string) {
	if n.IsNaN() || n.IsInf() {
		return "other", ""
	}
	s, rounded := pr.digits.FormatToString(n)
	e := 0
	if pr.notation == "compact" {
		e = number.CompactExponent(pr.dataLocale, pr.compactDisplay == "long", rounded)
	}
	return plural.Select(pr.dataLocale, pr.ordinal, s, e), s
}

// newPluralRules implements the Intl.PluralRules constructor.
func (r *realm) newPluralRules(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError(vm.CurrentRealm(), "constructor Intl.PluralRules requires 'new'")
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.pluralRulesProto })
	requested := canonicalizeLocaleList(vm, arg(args, 0))
	options := optionsObject(vm, arg(args, 1))
	bestFit := localeMatcher(vm, options)
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, nil)
	pr := &pluralRulesData{
		locale:     res.Locale,
		dataLocale: locale.DataLocale(res.DataLocale),
	}
	pr.ordinal = getStringOption(vm, options, "type", "cardinal", "cardinal", "ordinal") == "ordinal"
	pr.notation = getStringOption(vm, options, "notation", "standard", "standard", "scientific", "engineering", "compact")
	compactDisplay := getStringOption(vm, options, "compactDisplay", "short", "short", "long")
	if pr.notation == "compact" {
		pr.compactDisplay = compactDisplay
	}
	setDigitOptions(vm, &pr.digits, &pr.slots, options, 0, 3, pr.notation)
	return engine.ObjectValue(vm.NewIntlObject(proto, pr))
}

// thisPluralRules implements RequireInternalSlot for an Intl.PluralRules.
func thisPluralRules(vm *engine.VM, this engine.Value, name string) *pluralRulesData {
	pr, ok := engine.IntlData(this).(*pluralRulesData)
	if !ok {
		vm.ThrowTypeError(vm.CurrentRealm(), "Intl.PluralRules.prototype."+name+" called on an object that is not an Intl.PluralRules")
	}
	return pr
}

// initPluralRules defines Intl.PluralRules.
func (r *realm) initPluralRules(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("PluralRules", 0, r.newPluralRules)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("PluralRules"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.pluralRulesProto = proto

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		pr := thisPluralRules(vm, this, "resolvedOptions")
		out := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
		set := func(name string, v engine.Value) {
			vm.CreateDataPropertyOrThrow(out, engine.NamedKey(vm.Intern(wtf8.String(name))), v)
		}
		num := func(name string, v int) {
			set(name, engine.Number(float64(v)))
		}
		set("locale", vm.NewString(wtf8.String(pr.locale)))
		typ := "cardinal"
		if pr.ordinal {
			typ = "ordinal"
		}
		set("type", vm.NewString(wtf8.String(typ)))
		set("notation", vm.NewString(wtf8.String(pr.notation)))
		if pr.compactDisplay != "" {
			set("compactDisplay", vm.NewString(wtf8.String(pr.compactDisplay)))
		}
		d := &pr.digits
		num("minimumIntegerDigits", d.MinimumIntegerDigits)
		if pr.slots.hasFraction {
			num("minimumFractionDigits", d.MinimumFractionDigits)
			num("maximumFractionDigits", d.MaximumFractionDigits)
		}
		if pr.slots.hasSignificant {
			num("minimumSignificantDigits", d.MinimumSignificantDigits)
			num("maximumSignificantDigits", d.MaximumSignificantDigits)
		}
		set("pluralCategories", stringArray(vm, plural.Categories(pr.dataLocale, pr.ordinal)))
		num("roundingIncrement", d.RoundingIncrement)
		set("roundingMode", vm.NewString(wtf8.String(d.RoundingMode)))
		set("roundingPriority", vm.NewString(wtf8.String(pr.slots.priority)))
		tzd := "auto"
		if d.StripIfInteger {
			tzd = "stripIfInteger"
		}
		set("trailingZeroDisplay", vm.NewString(wtf8.String(tzd)))
		return engine.ObjectValue(out)
	})
	method(vm, proto, "select", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		pr := thisPluralRules(vm, this, "select")
		c, _ := pr.resolvePlural(toIntlMathematicalValue(vm, arg(args, 0)))
		return vm.NewString(wtf8.String(c))
	})
	method(vm, proto, "selectRange", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		pr := thisPluralRules(vm, this, "selectRange")
		start, end := arg(args, 0), arg(args, 1)
		if start.IsUndefined() || end.IsUndefined() {
			vm.ThrowTypeError(vm.CurrentRealm(), "selectRange requires a start and an end")
		}
		x := toIntlMathematicalValue(vm, start)
		y := toIntlMathematicalValue(vm, end)
		if x.IsNaN() || y.IsNaN() {
			vm.ThrowRangeError(vm.CurrentRealm(), "the start or the end of a range is NaN")
		}
		xc, xs := pr.resolvePlural(x)
		yc, ys := pr.resolvePlural(y)
		if xs == ys && !x.IsInf() && !y.IsInf() {
			return vm.NewString(wtf8.String(xc))
		}
		return vm.NewString(wtf8.String(plural.SelectRange(pr.dataLocale, xc, yc)))
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.PluralRules"), engine.FlagConfigurable)
}
