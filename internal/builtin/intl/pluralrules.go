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
func newPluralRules(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.PluralRules requires 'new'")
	}
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicIntlPluralRulesPrototype)
	requested := canonicalizeLocaleList(cx, arg(args, 0))
	options := optionsObject(cx, arg(args, 1))
	bestFit := localeMatcher(cx, options)
	res := locale.ResolveLocale(requested, defaultLocale(cx), bestFit, nil)
	pr := &pluralRulesData{
		locale:     res.Locale,
		dataLocale: locale.DataLocale(res.DataLocale),
	}
	pr.ordinal = getStringOption(cx, options, "type", "cardinal", "cardinal", "ordinal") == "ordinal"
	pr.notation = getStringOption(cx, options, "notation", "standard", "standard", "scientific", "engineering", "compact")
	compactDisplay := getStringOption(cx, options, "compactDisplay", "short", "short", "long")
	if pr.notation == "compact" {
		pr.compactDisplay = compactDisplay
	}
	setDigitOptions(cx, &pr.digits, &pr.slots, options, 0, 3, pr.notation)
	return engine.ObjectValue(cx.NewIntlObject(proto, pr))
}

// thisPluralRules implements RequireInternalSlot for an Intl.PluralRules.
func thisPluralRules(cx *engine.ExecutionContext, this engine.Value, name string) *pluralRulesData {
	pr, ok := engine.IntlData(this).(*pluralRulesData)
	if !ok {
		cx.ThrowTypeError("Intl.PluralRules.prototype." + name + " called on an object that is not an Intl.PluralRules")
	}
	return pr
}

var (
	nativeNewPluralRules             = engine.RegisterNative(newPluralRules)
	nativePluralRulesResolvedOptions = engine.RegisterNative(pluralRulesResolvedOptions)
	nativePluralRulesSelect          = engine.RegisterNative(pluralRulesSelect)
	nativePluralRulesSelectRange     = engine.RegisterNative(pluralRulesSelectRange)
)

// initPluralRules defines Intl.PluralRules.
func initPluralRules(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := constructor(cx, ns, "PluralRules", 0, engine.IntrinsicIntlPluralRulesPrototype, nativeNewPluralRules)

	method(cx, ctor, "supportedLocalesOf", 1, nativeSupportedLocalesOf)
	method(cx, proto, "resolvedOptions", 0, nativePluralRulesResolvedOptions)
	method(cx, proto, "select", 1, nativePluralRulesSelect)
	method(cx, proto, "selectRange", 2, nativePluralRulesSelectRange)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.PluralRules"), engine.FlagConfigurable)
}

func pluralRulesResolvedOptions(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	pr := thisPluralRules(cx, this, "resolvedOptions")
	out := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	set := func(name string, v engine.Value) {
		cx.CreateDataPropertyOrThrow(out, engine.NamedKey(cx.Intern(wtf8.String(name))), v)
	}
	num := func(name string, v int) {
		set(name, engine.Number(float64(v)))
	}
	set("locale", cx.NewString(wtf8.String(pr.locale)))
	typ := "cardinal"
	if pr.ordinal {
		typ = "ordinal"
	}
	set("type", cx.NewString(wtf8.String(typ)))
	set("notation", cx.NewString(wtf8.String(pr.notation)))
	if pr.compactDisplay != "" {
		set("compactDisplay", cx.NewString(wtf8.String(pr.compactDisplay)))
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
	set("pluralCategories", stringArray(cx, plural.Categories(pr.dataLocale, pr.ordinal)))
	num("roundingIncrement", d.RoundingIncrement)
	set("roundingMode", cx.NewString(wtf8.String(d.RoundingMode)))
	set("roundingPriority", cx.NewString(wtf8.String(pr.slots.priority)))
	tzd := "auto"
	if d.StripIfInteger {
		tzd = "stripIfInteger"
	}
	set("trailingZeroDisplay", cx.NewString(wtf8.String(tzd)))
	return engine.ObjectValue(out)
}

func pluralRulesSelect(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	pr := thisPluralRules(cx, this, "select")
	c, _ := pr.resolvePlural(toIntlMathematicalValue(cx, arg(args, 0)))
	return cx.NewString(wtf8.String(c))
}

func pluralRulesSelectRange(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	pr := thisPluralRules(cx, this, "selectRange")
	start, end := arg(args, 0), arg(args, 1)
	if start.IsUndefined() || end.IsUndefined() {
		cx.ThrowTypeError("selectRange requires a start and an end")
	}
	x := toIntlMathematicalValue(cx, start)
	y := toIntlMathematicalValue(cx, end)
	if x.IsNaN() || y.IsNaN() {
		cx.ThrowRangeError("the start or the end of a range is NaN")
	}
	xc, xs := pr.resolvePlural(x)
	yc, ys := pr.resolvePlural(y)
	if xs == ys && !x.IsInf() && !y.IsInf() {
		return cx.NewString(wtf8.String(xc))
	}
	return cx.NewString(wtf8.String(plural.SelectRange(pr.dataLocale, xc, yc)))
}
