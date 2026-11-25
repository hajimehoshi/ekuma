// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"math"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/intl/number"
	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// numberFormatData holds the internal slots of an Intl.NumberFormat object.
type numberFormatData struct {
	locale string
	opts   number.Options
	digits digitSlots
	// useGrouping is false when [[UseGrouping]] is false.
	useGrouping bool
	f           *number.Formatter
	boundFormat *engine.Object
}

// digitSlots tells which digit slots SetNumberFormatDigitOptions set, and the resolved
// rounding priority.
type digitSlots struct {
	hasFraction    bool
	hasSignificant bool
	priority       string
}

// optionsObject implements CoerceOptionsToObject.
func optionsObject(cx *engine.ExecutionContext, v engine.Value) *engine.Object {
	if v.IsUndefined() {
		return cx.NewObject(nil)
	}
	return cx.ToObject(v)
}

// getNumberOption implements GetNumberOption, with -1 for an undefined fallback.
func getNumberOption(cx *engine.ExecutionContext, options *engine.Object, name string, minimum, maximum, fallback int) int {
	return defaultNumberOption(cx, cx.Get(options, engine.NamedKey(cx.Intern(wtf8.String(name)))), name, minimum, maximum, fallback)
}

// defaultNumberOption implements DefaultNumberOption, with -1 for an undefined fallback.
func defaultNumberOption(cx *engine.ExecutionContext, v engine.Value, name string, minimum, maximum, fallback int) int {
	if v.IsUndefined() {
		return fallback
	}
	n := cx.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < float64(minimum) || n > float64(maximum) {
		cx.ThrowRangeError(name + " value is out of range")
	}
	return int(math.Trunc(n))
}

// getStringOption implements GetOption for a string option with a default.
func getStringOption(cx *engine.ExecutionContext, options *engine.Object, name, fallback string, values ...string) string {
	if s, ok := getOption(cx, options, name, values...); ok {
		return s
	}
	return fallback
}

// localeMatcher reads the localeMatcher option and reports whether it is "best fit".
func localeMatcher(cx *engine.ExecutionContext, options *engine.Object) bool {
	return getStringOption(cx, options, "localeMatcher", "best fit", "lookup", "best fit") == "best fit"
}

// supportedLocalesOf is the method supportedLocalesOf of the constructors. It implements
// FilterLocales.
func supportedLocalesOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requested := canonicalizeLocaleList(cx, arg(args, 0))
	bestFit := localeMatcher(cx, optionsObject(cx, arg(args, 1)))
	return stringArray(cx, locale.SupportedLocales(requested, bestFit))
}

// setDigitOptions implements SetNumberFormatDigitOptions.
func setDigitOptions(cx *engine.ExecutionContext, d *number.Digits, slots *digitSlots, options *engine.Object, minFDDefault, maxFDDefault int, notation string) {
	get := func(name string) engine.Value {
		return cx.Get(options, engine.NamedKey(cx.Intern(wtf8.String(name))))
	}
	minInt := getNumberOption(cx, options, "minimumIntegerDigits", 1, 21, 1)
	minFD := get("minimumFractionDigits")
	maxFD := get("maximumFractionDigits")
	minSD := get("minimumSignificantDigits")
	maxSD := get("maximumSignificantDigits")
	inc := getNumberOption(cx, options, "roundingIncrement", 1, 5000, 1)
	if !slices.Contains([]int{1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000, 2500, 5000}, inc) {
		cx.ThrowRangeError("invalid roundingIncrement")
	}
	mode := getStringOption(cx, options, "roundingMode", "halfExpand", "ceil", "floor", "expand", "trunc", "halfCeil", "halfFloor", "halfExpand", "halfTrunc", "halfEven")
	priority := getStringOption(cx, options, "roundingPriority", "auto", "auto", "morePrecision", "lessPrecision")
	tzd := getStringOption(cx, options, "trailingZeroDisplay", "auto", "auto", "stripIfInteger")
	if inc != 1 {
		maxFDDefault = minFDDefault
	}
	d.MinimumIntegerDigits = minInt
	d.RoundingIncrement = inc
	d.RoundingMode = mode
	d.StripIfInteger = tzd == "stripIfInteger"
	hasSD := !minSD.IsUndefined() || !maxSD.IsUndefined()
	hasFD := !minFD.IsUndefined() || !maxFD.IsUndefined()
	needSD, needFD := true, true
	if priority == "auto" {
		needSD = hasSD
		needFD = !hasSD && (hasFD || notation != "compact")
	}
	if needSD {
		if hasSD {
			d.MinimumSignificantDigits = defaultNumberOption(cx, minSD, "minimumSignificantDigits", 1, 21, 1)
			d.MaximumSignificantDigits = defaultNumberOption(cx, maxSD, "maximumSignificantDigits", d.MinimumSignificantDigits, 21, 21)
		} else {
			d.MinimumSignificantDigits = 1
			d.MaximumSignificantDigits = 21
		}
	}
	if needFD {
		if hasFD {
			lo := defaultNumberOption(cx, minFD, "minimumFractionDigits", 0, 100, -1)
			hi := defaultNumberOption(cx, maxFD, "maximumFractionDigits", 0, 100, -1)
			switch {
			case lo < 0:
				lo = min(minFDDefault, hi)
			case hi < 0:
				hi = max(maxFDDefault, lo)
			case lo > hi:
				cx.ThrowRangeError("minimumFractionDigits is greater than maximumFractionDigits")
			}
			d.MinimumFractionDigits = lo
			d.MaximumFractionDigits = hi
		} else {
			d.MinimumFractionDigits = minFDDefault
			d.MaximumFractionDigits = maxFDDefault
		}
	}
	slots.hasFraction = needFD
	slots.hasSignificant = needSD
	switch {
	case !needSD && !needFD:
		d.MinimumFractionDigits = 0
		d.MaximumFractionDigits = 0
		d.MinimumSignificantDigits = 1
		d.MaximumSignificantDigits = 2
		d.RoundingType = number.MorePrecision
		slots.priority = "morePrecision"
		slots.hasFraction = true
		slots.hasSignificant = true
	case priority == "morePrecision":
		d.RoundingType = number.MorePrecision
		slots.priority = "morePrecision"
	case priority == "lessPrecision":
		d.RoundingType = number.LessPrecision
		slots.priority = "lessPrecision"
	case hasSD:
		d.RoundingType = number.SignificantDigits
		slots.priority = "auto"
	default:
		d.RoundingType = number.FractionDigits
		slots.priority = "auto"
	}
	if inc != 1 {
		if d.RoundingType != number.FractionDigits {
			cx.ThrowTypeError("roundingIncrement requires fraction digits rounding")
		}
		if d.MaximumFractionDigits != d.MinimumFractionDigits {
			cx.ThrowRangeError("roundingIncrement requires equal minimumFractionDigits and maximumFractionDigits")
		}
	}
}

// isWellFormedCurrencyCode implements IsWellFormedCurrencyCode.
func isWellFormedCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := range 3 {
		c := s[i] | 0x20
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// isWellFormedUnitIdentifier implements IsWellFormedUnitIdentifier.
func isWellFormedUnitIdentifier(s string) bool {
	if _, ok := slices.BinarySearch(sanctionedUnits, s); ok {
		return true
	}
	num, den, ok := strings.Cut(s, "-per-")
	if !ok {
		return false
	}
	_, okN := slices.BinarySearch(sanctionedUnits, num)
	_, okD := slices.BinarySearch(sanctionedUnits, den)
	return okN && okD
}

// numberingSystemsOf returns the numbering systems that NumberFormat supports for the
// available locale l, its default first.
func numberingSystemsOf(l string) []string {
	def := locale.NumberingSystemOf(locale.DataLocale(l))
	return append([]string{def}, locale.NumberingSystems()...)
}

// numberingSystemOption reads the numberingSystem option.
func numberingSystemOption(cx *engine.ExecutionContext, options *engine.Object) (string, bool) {
	s, ok := getOption(cx, options, "numberingSystem")
	if ok && !localetag.IsType(strings.ToLower(s)) {
		cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for numberingSystem")
	}
	if ok {
		for i := range len(s) {
			if s[i] >= 0x80 {
				cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for numberingSystem")
			}
		}
	}
	return s, ok
}

func resolveNumberingSystemLocale(cx *engine.ExecutionContext, requested []string, options *engine.Object) locale.Resolution {
	bestFit := localeMatcher(cx, options)
	nu, hasNu := numberingSystemOption(cx, options)
	return locale.ResolveLocale(requested, defaultLocale(cx), bestFit, []locale.ExtensionKey{{
		Key:       "nu",
		Option:    nu,
		HasOption: hasNu,
		Values:    numberingSystemsOf,
	}})
}

// createNumberFormat implements the steps of the Intl.NumberFormat constructor that resolve
// the internal slots.
func createNumberFormat(cx *engine.ExecutionContext, locales, opts engine.Value) *numberFormatData {
	requested := canonicalizeLocaleList(cx, locales)
	options := optionsObject(cx, opts)
	res := resolveNumberingSystemLocale(cx, requested, options)
	nf := &numberFormatData{
		locale: res.Locale,
	}
	o := &nf.opts
	o.Locale = res.DataLocale
	o.NumberingSystem = res.Values[0]

	// SetNumberFormatUnitOptions.
	o.Style = getStringOption(cx, options, "style", "decimal", "decimal", "percent", "currency", "unit")
	currency, hasCurrency := getOption(cx, options, "currency")
	if !hasCurrency {
		if o.Style == "currency" {
			cx.ThrowTypeError("currency is required with the currency style")
		}
	} else if !isWellFormedCurrencyCode(currency) {
		cx.ThrowRangeError("invalid currency code: " + cx.DisplayString(cx.NewString(wtf8.String(currency))))
	}
	currencyDisplay := getStringOption(cx, options, "currencyDisplay", "symbol", "code", "symbol", "narrowSymbol", "name")
	currencySign := getStringOption(cx, options, "currencySign", "standard", "standard", "accounting")
	unit, hasUnit := getOption(cx, options, "unit")
	if !hasUnit {
		if o.Style == "unit" {
			cx.ThrowTypeError("unit is required with the unit style")
		}
	} else if !isWellFormedUnitIdentifier(unit) {
		cx.ThrowRangeError("invalid unit: " + cx.DisplayString(cx.NewString(wtf8.String(unit))))
	}
	unitDisplay := getStringOption(cx, options, "unitDisplay", "short", "short", "narrow", "long")
	if o.Style == "currency" {
		o.Currency = strings.ToUpper(currency)
		o.CurrencyDisplay = currencyDisplay
		o.CurrencySign = currencySign
	}
	if o.Style == "unit" {
		o.Unit = unit
		o.UnitDisplay = unitDisplay
	}

	o.Notation = getStringOption(cx, options, "notation", "standard", "standard", "scientific", "engineering", "compact")
	minFD, maxFD := 0, 3
	switch {
	case o.Style == "currency" && o.Notation == "standard":
		minFD = number.CurrencyDigits(o.Currency)
		maxFD = minFD
	case o.Style == "percent":
		minFD, maxFD = 0, 0
	}
	setDigitOptions(cx, &o.Digits, &nf.digits, options, minFD, maxFD, o.Notation)
	compactDisplay := getStringOption(cx, options, "compactDisplay", "short", "short", "long")
	defaultUseGrouping := "auto"
	if o.Notation == "compact" {
		o.CompactDisplay = compactDisplay
		defaultUseGrouping = "min2"
	}
	o.UseGrouping = defaultUseGrouping
	nf.useGrouping = true
	if v := cx.Get(options, engine.NamedKey(cx.Intern("useGrouping"))); !v.IsUndefined() {
		switch {
		case v.IsBool() && engine.ToBoolean(v):
			o.UseGrouping = "always"
		case !engine.ToBoolean(v):
			o.UseGrouping = ""
			nf.useGrouping = false
		default:
			s := wtf8.ToValidUTF8(cx.ToString(v))
			if !slices.Contains([]string{"min2", "auto", "always", "true", "false"}, s) {
				cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for useGrouping")
			}
			if s != "true" && s != "false" {
				o.UseGrouping = s
			}
		}
	}
	o.SignDisplay = getStringOption(cx, options, "signDisplay", "auto", "auto", "never", "always", "exceptZero", "negative")
	nf.f = number.NewFormatter(o)
	return nf
}

// toIntlMathematicalValue implements ToIntlMathematicalValue.
func toIntlMathematicalValue(cx *engine.ExecutionContext, v engine.Value) number.Decimal {
	p := cx.ToPrimitive(v, engine.HintNumber)
	switch {
	case p.IsBigInt():
		return number.FromInteger(string(cx.BigIntToString(p, 10)))
	case p.IsString():
		return number.Parse(wtf8.ToValidUTF8(cx.ToString(p)))
	}
	return number.FromFloat(cx.ToNumber(p))
}

// newNumberFormat implements the Intl.NumberFormat constructor.
func newNumberFormat(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicIntlNumberFormatPrototype)
	nf := cx.NewIntlObject(proto, createNumberFormat(cx, arg(args, 0), arg(args, 1)))
	// ChainNumberFormat.
	if nt == nil && cx.OrdinaryHasInstance(engine.ObjectValue(cx.Intrinsic(engine.IntrinsicIntlNumberFormat)), this) {
		var d engine.PropDesc
		d.SetValue(engine.ObjectValue(nf))
		d.SetWritable(false)
		d.SetEnumerable(false)
		d.SetConfigurable(false)
		cx.DefinePropertyOrThrow(this.Object(), engine.NamedKey(cx.IntlFallbackSymbol()), d)
		return this
	}
	return engine.ObjectValue(nf)
}

// unwrapNumberFormat implements UnwrapNumberFormat followed by the check of the internal
// slot.
func unwrapNumberFormat(cx *engine.ExecutionContext, this engine.Value, name string) *numberFormatData {
	if !this.IsObject() {
		cx.ThrowTypeError("Intl.NumberFormat.prototype." + name + " called on a value that is not an object")
	}
	if nf, ok := engine.IntlData(this).(*numberFormatData); ok {
		return nf
	}
	if cx.OrdinaryHasInstance(engine.ObjectValue(cx.Intrinsic(engine.IntrinsicIntlNumberFormat)), this) {
		if nf, ok := engine.IntlData(cx.Get(this.Object(), engine.NamedKey(cx.IntlFallbackSymbol()))).(*numberFormatData); ok {
			return nf
		}
	}
	cx.ThrowTypeError("Intl.NumberFormat.prototype." + name + " called on an object that is not an Intl.NumberFormat")
	return nil
}

// thisNumberFormat implements RequireInternalSlot for an Intl.NumberFormat.
func thisNumberFormat(cx *engine.ExecutionContext, this engine.Value, name string) *numberFormatData {
	nf, ok := engine.IntlData(this).(*numberFormatData)
	if !ok {
		cx.ThrowTypeError("Intl.NumberFormat.prototype." + name + " called on an object that is not an Intl.NumberFormat")
	}
	return nf
}

// partsArray returns the parts as an array of objects with the properties "type" and
// "value", and "source" for range parts.
func partsArray(cx *engine.ExecutionContext, n int, part func(i int) (typ, value, source string)) engine.Value {
	keys := [...]engine.PropKey{
		engine.NamedKey(cx.Intern("type")),
		engine.NamedKey(cx.Intern("value")),
		engine.NamedKey(cx.Intern("source")),
	}
	vals := make([]engine.Value, n)
	var p engine.Pacer
	for i := range n {
		p.Step(cx)
		typ, value, source := part(i)
		props := [3]engine.Value{cx.NewString(wtf8.String(typ)), cx.NewString(wtf8.String(value))}
		propertyCount := 2
		if source != "" {
			props[2] = cx.NewString(wtf8.String(source))
			propertyCount++
		}
		vals[i] = engine.ObjectValue(cx.NewObjectFromProperties(keys[:propertyCount], props[:propertyCount]))
	}
	return cx.NewArrayOf(vals)
}

// formatRange checks and converts the arguments start and end of formatRange and
// formatRangeToParts, and implements PartitionNumberRangePattern for them.
func (nf *numberFormatData) formatRange(cx *engine.ExecutionContext, args []engine.Value) []number.RangePart {
	start, end := arg(args, 0), arg(args, 1)
	if start.IsUndefined() || end.IsUndefined() {
		cx.ThrowTypeError("formatRange requires a start and an end")
	}
	x := toIntlMathematicalValue(cx, start)
	y := toIntlMathematicalValue(cx, end)
	if x.IsNaN() || y.IsNaN() {
		cx.ThrowRangeError("the start or the end of a range is NaN")
	}
	return nf.f.FormatRange(x, y, cx.Poll)
}

// numberFormatFunction is the code of the number format functions. Their state is a
// *numberFormatData.
func numberFormatFunction(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nf := cx.ActiveNativeState().(*numberFormatData)
	x := toIntlMathematicalValue(cx, arg(args, 0))
	return formattedString(cx, nf.f.FormatToString(x, cx.Poll))
}

var (
	nativeNewNumberFormat                = engine.RegisterNative(newNumberFormat)
	nativeNumberFormatResolvedOptions    = engine.RegisterNative(numberFormatResolvedOptions)
	nativeNumberFormatFormat             = engine.RegisterNative(numberFormatFormat)
	nativeNumberFormatFormatRange        = engine.RegisterNative(numberFormatFormatRange)
	nativeNumberFormatFormatRangeToParts = engine.RegisterNative(numberFormatFormatRangeToParts)
	nativeNumberFormatFormatToParts      = engine.RegisterNative(numberFormatFormatToParts)
)

// initNumberFormat defines Intl.NumberFormat.
func initNumberFormat(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := constructor(cx, ns, "NumberFormat", 0, engine.IntrinsicIntlNumberFormatPrototype, nativeNewNumberFormat)
	cx.SetIntrinsic(engine.IntrinsicIntlNumberFormat, ctor)

	method(cx, ctor, "supportedLocalesOf", 1, nativeSupportedLocalesOf)
	method(cx, proto, "resolvedOptions", 0, nativeNumberFormatResolvedOptions)
	getter(cx, proto, "format", nativeNumberFormatFormat)
	method(cx, proto, "formatRange", 2, nativeNumberFormatFormatRange)
	method(cx, proto, "formatRangeToParts", 2, nativeNumberFormatFormatRangeToParts)
	method(cx, proto, "formatToParts", 1, nativeNumberFormatFormatToParts)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.NumberFormat"), engine.FlagConfigurable)
}

func numberFormatResolvedOptions(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nf := unwrapNumberFormat(cx, this, "resolvedOptions")
	return nf.resolvedOptions(cx)
}

var nativeNumberFormatFunction = engine.RegisterNative(numberFormatFunction)

func numberFormatFormat(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nf := unwrapNumberFormat(cx, this, "format")
	if nf.boundFormat == nil {
		nf.boundFormat = cx.NewNativeWithState("", 1, nativeNumberFormatFunction, nf)
	}
	return engine.ObjectValue(nf.boundFormat)
}

func numberFormatFormatRange(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nf := thisNumberFormat(cx, this, "formatRange")
	var b strings.Builder
	for _, p := range nf.formatRange(cx, args) {
		b.WriteString(p.Value)
	}
	checkStringLength(cx, b.Len())
	return cx.NewString(wtf8.String(b.String()))
}

func numberFormatFormatRangeToParts(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nf := thisNumberFormat(cx, this, "formatRangeToParts")
	parts := nf.formatRange(cx, args)
	return partsArray(cx, len(parts), func(i int) (string, string, string) {
		return parts[i].Type, parts[i].Value, parts[i].Source
	})
}

func numberFormatFormatToParts(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nf := thisNumberFormat(cx, this, "formatToParts")
	parts := nf.f.Format(toIntlMathematicalValue(cx, arg(args, 0)), cx.Poll)
	return partsArray(cx, len(parts), func(i int) (string, string, string) {
		return parts[i].Type, parts[i].Value, ""
	})
}

// resolvedOptions implements Intl.NumberFormat.prototype.resolvedOptions after the check of
// the internal slot.
func (nf *numberFormatData) resolvedOptions(cx *engine.ExecutionContext) engine.Value {
	o := &nf.opts
	out := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	set := func(name string, v engine.Value) {
		cx.CreateDataPropertyOrThrow(out, engine.NamedKey(cx.Intern(wtf8.String(name))), v)
	}
	str := func(name, v string) {
		if v != "" {
			set(name, cx.NewString(wtf8.String(v)))
		}
	}
	num := func(name string, v int) {
		set(name, engine.Number(float64(v)))
	}
	str("locale", nf.locale)
	str("numberingSystem", o.NumberingSystem)
	str("style", o.Style)
	str("currency", o.Currency)
	str("currencyDisplay", o.CurrencyDisplay)
	str("currencySign", o.CurrencySign)
	str("unit", o.Unit)
	str("unitDisplay", o.UnitDisplay)
	num("minimumIntegerDigits", o.MinimumIntegerDigits)
	if nf.digits.hasFraction {
		num("minimumFractionDigits", o.MinimumFractionDigits)
		num("maximumFractionDigits", o.MaximumFractionDigits)
	}
	if nf.digits.hasSignificant {
		num("minimumSignificantDigits", o.MinimumSignificantDigits)
		num("maximumSignificantDigits", o.MaximumSignificantDigits)
	}
	if nf.useGrouping {
		str("useGrouping", o.UseGrouping)
	} else {
		set("useGrouping", engine.Bool(false))
	}
	str("notation", o.Notation)
	str("compactDisplay", o.CompactDisplay)
	str("signDisplay", o.SignDisplay)
	num("roundingIncrement", o.RoundingIncrement)
	str("roundingMode", o.RoundingMode)
	str("roundingPriority", nf.digits.priority)
	if o.StripIfInteger {
		str("trailingZeroDisplay", "stripIfInteger")
	} else {
		str("trailingZeroDisplay", "auto")
	}
	return engine.ObjectValue(out)
}

// formattedString returns the formatted string s, and throws a RangeError instead when it is
// longer than MaxStringLength.
func formattedString(cx *engine.ExecutionContext, s string) engine.Value {
	checkStringLength(cx, len(s))
	return cx.NewString(wtf8.String(s))
}

func formatToLocaleString(cx *engine.ExecutionContext, args []engine.Value, x number.Decimal) engine.Value {
	nf := createNumberFormat(cx, arg(args, 0), arg(args, 1))
	return formattedString(cx, nf.f.FormatToString(x, cx.Poll))
}

var (
	nativeNumberToLocaleString = engine.RegisterNative(numberToLocaleString)
	nativeBigIntToLocaleString = engine.RegisterNative(bigIntToLocaleString)
)

// initToLocaleString replaces Number.prototype.toLocaleString and
// BigInt.prototype.toLocaleString with the versions of ECMA-402.
func initToLocaleString(cx *engine.ExecutionContext) {
	method(cx, cx.Intrinsic(engine.IntrinsicNumberPrototype), "toLocaleString", 0, nativeNumberToLocaleString)
	method(cx, cx.Intrinsic(engine.IntrinsicBigIntPrototype), "toLocaleString", 0, nativeBigIntToLocaleString)
}

func numberToLocaleString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	x, ok := engine.ThisNumberValue(this)
	if !ok {
		cx.ThrowTypeError("Number.prototype.toLocaleString requires that 'this' be a Number")
	}
	return formatToLocaleString(cx, args, number.FromFloat(x))
}

func bigIntToLocaleString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	x, ok := engine.ThisBigIntValue(this)
	if !ok {
		cx.ThrowTypeError("BigInt.prototype.toLocaleString requires that 'this' be a BigInt")
	}
	return formatToLocaleString(cx, args, number.FromInteger(string(cx.BigIntToString(x, 10))))
}
