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
func optionsObject(vm *engine.VM, v engine.Value) *engine.Object {
	if v.IsUndefined() {
		return vm.NewObject(nil)
	}
	return vm.ToObject(v)
}

// getNumberOption implements GetNumberOption, with -1 for an undefined fallback.
func getNumberOption(vm *engine.VM, options *engine.Object, name string, minimum, maximum, fallback int) int {
	return defaultNumberOption(vm, vm.Get(options, engine.NamedKey(vm.Intern(name))), name, minimum, maximum, fallback)
}

// defaultNumberOption implements DefaultNumberOption, with -1 for an undefined fallback.
func defaultNumberOption(vm *engine.VM, v engine.Value, name string, minimum, maximum, fallback int) int {
	if v.IsUndefined() {
		return fallback
	}
	n := vm.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < float64(minimum) || n > float64(maximum) {
		vm.ThrowRangeError(name + " value is out of range")
	}
	return int(math.Trunc(n))
}

// getStringOption implements GetOption for a string option with a default.
func getStringOption(vm *engine.VM, options *engine.Object, name, fallback string, values ...string) string {
	if s, ok := getOption(vm, options, name, values...); ok {
		return s
	}
	return fallback
}

// localeMatcher reads the localeMatcher option and reports whether it is "best fit".
func localeMatcher(vm *engine.VM, options *engine.Object) bool {
	return getStringOption(vm, options, "localeMatcher", "best fit", "lookup", "best fit") == "best fit"
}

// supportedLocalesOf implements FilterLocales for the arguments of supportedLocalesOf.
func supportedLocalesOf(vm *engine.VM, args []engine.Value) engine.Value {
	requested := canonicalizeLocaleList(vm, arg(args, 0))
	localeMatcher(vm, optionsObject(vm, arg(args, 1)))
	return stringArray(vm, locale.SupportedLocales(requested))
}

// setDigitOptions implements SetNumberFormatDigitOptions.
func setDigitOptions(vm *engine.VM, d *number.Digits, slots *digitSlots, options *engine.Object, minFDDefault, maxFDDefault int, notation string) {
	get := func(name string) engine.Value {
		return vm.Get(options, engine.NamedKey(vm.Intern(name)))
	}
	minInt := getNumberOption(vm, options, "minimumIntegerDigits", 1, 21, 1)
	minFD := get("minimumFractionDigits")
	maxFD := get("maximumFractionDigits")
	minSD := get("minimumSignificantDigits")
	maxSD := get("maximumSignificantDigits")
	inc := getNumberOption(vm, options, "roundingIncrement", 1, 5000, 1)
	if !slices.Contains([]int{1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000, 2500, 5000}, inc) {
		vm.ThrowRangeError("invalid roundingIncrement")
	}
	mode := getStringOption(vm, options, "roundingMode", "halfExpand", "ceil", "floor", "expand", "trunc", "halfCeil", "halfFloor", "halfExpand", "halfTrunc", "halfEven")
	priority := getStringOption(vm, options, "roundingPriority", "auto", "auto", "morePrecision", "lessPrecision")
	tzd := getStringOption(vm, options, "trailingZeroDisplay", "auto", "auto", "stripIfInteger")
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
			d.MinimumSignificantDigits = defaultNumberOption(vm, minSD, "minimumSignificantDigits", 1, 21, 1)
			d.MaximumSignificantDigits = defaultNumberOption(vm, maxSD, "maximumSignificantDigits", d.MinimumSignificantDigits, 21, 21)
		} else {
			d.MinimumSignificantDigits = 1
			d.MaximumSignificantDigits = 21
		}
	}
	if needFD {
		if hasFD {
			lo := defaultNumberOption(vm, minFD, "minimumFractionDigits", 0, 100, -1)
			hi := defaultNumberOption(vm, maxFD, "maximumFractionDigits", 0, 100, -1)
			switch {
			case lo < 0:
				lo = min(minFDDefault, hi)
			case hi < 0:
				hi = max(maxFDDefault, lo)
			case lo > hi:
				vm.ThrowRangeError("minimumFractionDigits is greater than maximumFractionDigits")
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
			vm.ThrowTypeError("roundingIncrement requires fraction digits rounding")
		}
		if d.MaximumFractionDigits != d.MinimumFractionDigits {
			vm.ThrowRangeError("roundingIncrement requires equal minimumFractionDigits and maximumFractionDigits")
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
func numberingSystemOption(vm *engine.VM, options *engine.Object) (string, bool) {
	s, ok := getOption(vm, options, "numberingSystem")
	if ok && !localetag.IsType(strings.ToLower(s)) {
		vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for numberingSystem")
	}
	if ok {
		for i := range len(s) {
			if s[i] >= 0x80 {
				vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for numberingSystem")
			}
		}
	}
	return s, ok
}

// createNumberFormat implements the steps of the Intl.NumberFormat constructor that resolve
// the internal slots.
func createNumberFormat(vm *engine.VM, locales, opts engine.Value) *numberFormatData {
	requested := canonicalizeLocaleList(vm, locales)
	options := optionsObject(vm, opts)
	bestFit := localeMatcher(vm, options)
	nu, hasNu := numberingSystemOption(vm, options)
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, []locale.ExtensionKey{{
		Key:       "nu",
		Option:    nu,
		HasOption: hasNu,
		Values:    numberingSystemsOf,
	}})
	nf := &numberFormatData{
		locale: res.Locale,
	}
	o := &nf.opts
	o.Locale = res.DataLocale
	o.NumberingSystem = res.Values[0]

	// SetNumberFormatUnitOptions.
	o.Style = getStringOption(vm, options, "style", "decimal", "decimal", "percent", "currency", "unit")
	currency, hasCurrency := getOption(vm, options, "currency")
	if !hasCurrency {
		if o.Style == "currency" {
			vm.ThrowTypeError("currency is required with the currency style")
		}
	} else if !isWellFormedCurrencyCode(currency) {
		vm.ThrowRangeError("invalid currency code: " + currency)
	}
	currencyDisplay := getStringOption(vm, options, "currencyDisplay", "symbol", "code", "symbol", "narrowSymbol", "name")
	currencySign := getStringOption(vm, options, "currencySign", "standard", "standard", "accounting")
	unit, hasUnit := getOption(vm, options, "unit")
	if !hasUnit {
		if o.Style == "unit" {
			vm.ThrowTypeError("unit is required with the unit style")
		}
	} else if !isWellFormedUnitIdentifier(unit) {
		vm.ThrowRangeError("invalid unit: " + unit)
	}
	unitDisplay := getStringOption(vm, options, "unitDisplay", "short", "short", "narrow", "long")
	if o.Style == "currency" {
		o.Currency = strings.ToUpper(currency)
		o.CurrencyDisplay = currencyDisplay
		o.CurrencySign = currencySign
	}
	if o.Style == "unit" {
		o.Unit = unit
		o.UnitDisplay = unitDisplay
	}

	o.Notation = getStringOption(vm, options, "notation", "standard", "standard", "scientific", "engineering", "compact")
	minFD, maxFD := 0, 3
	switch {
	case o.Style == "currency" && o.Notation == "standard":
		minFD = number.CurrencyDigits(o.Currency)
		maxFD = minFD
	case o.Style == "percent":
		minFD, maxFD = 0, 0
	}
	setDigitOptions(vm, &o.Digits, &nf.digits, options, minFD, maxFD, o.Notation)
	compactDisplay := getStringOption(vm, options, "compactDisplay", "short", "short", "long")
	defaultUseGrouping := "auto"
	if o.Notation == "compact" {
		o.CompactDisplay = compactDisplay
		defaultUseGrouping = "min2"
	}
	o.UseGrouping = defaultUseGrouping
	nf.useGrouping = true
	if v := vm.Get(options, engine.NamedKey(vm.Intern("useGrouping"))); !v.IsUndefined() {
		switch {
		case v.IsBool() && engine.ToBoolean(v):
			o.UseGrouping = "always"
		case !engine.ToBoolean(v):
			o.UseGrouping = ""
			nf.useGrouping = false
		default:
			s := vm.ToString(v)
			if !slices.Contains([]string{"min2", "auto", "always", "true", "false"}, s) {
				vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for useGrouping")
			}
			if s != "true" && s != "false" {
				o.UseGrouping = s
			}
		}
	}
	o.SignDisplay = getStringOption(vm, options, "signDisplay", "auto", "auto", "never", "always", "exceptZero", "negative")
	nf.f = number.NewFormatter(o)
	return nf
}

// toIntlMathematicalValue implements ToIntlMathematicalValue.
func toIntlMathematicalValue(vm *engine.VM, v engine.Value) number.Decimal {
	p := vm.ToPrimitive(v, engine.HintNumber)
	switch {
	case p.IsBigInt():
		return number.FromInteger(vm.BigIntToString(p, 10))
	case p.IsString():
		return number.Parse(vm.ToString(p))
	}
	return number.FromFloat(vm.ToNumber(p))
}

// newNumberFormat implements the Intl.NumberFormat constructor.
func (r *realm) newNumberFormat(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	newTarget := nt
	if newTarget == nil {
		newTarget = r.numberFormatCtor
	}
	proto := protoFor(vm, newTarget, func(r *realm) *engine.Object { return r.numberFormatProto })
	nf := vm.NewIntlObject(proto, createNumberFormat(vm, arg(args, 0), arg(args, 1)))
	// ChainNumberFormat.
	if nt == nil && vm.OrdinaryHasInstance(engine.ObjectValue(r.numberFormatCtor), this) {
		var d engine.PropDesc
		d.SetValue(engine.ObjectValue(nf))
		d.SetWritable(false)
		d.SetEnumerable(false)
		d.SetConfigurable(false)
		vm.DefinePropertyOrThrow(this.Object(), engine.NamedKey(r.fallbackSymbol), d)
		return this
	}
	return engine.ObjectValue(nf)
}

// unwrapNumberFormat implements UnwrapNumberFormat followed by the check of the internal
// slot.
func (r *realm) unwrapNumberFormat(vm *engine.VM, this engine.Value, name string) *numberFormatData {
	if !this.IsObject() {
		vm.ThrowTypeError("Intl.NumberFormat.prototype." + name + " called on a value that is not an object")
	}
	if nf, ok := engine.IntlData(this).(*numberFormatData); ok {
		return nf
	}
	if vm.OrdinaryHasInstance(engine.ObjectValue(r.numberFormatCtor), this) {
		if nf, ok := engine.IntlData(vm.Get(this.Object(), engine.NamedKey(r.fallbackSymbol))).(*numberFormatData); ok {
			return nf
		}
	}
	vm.ThrowTypeError("Intl.NumberFormat.prototype." + name + " called on an object that is not an Intl.NumberFormat")
	return nil
}

// thisNumberFormat implements RequireInternalSlot for an Intl.NumberFormat.
func thisNumberFormat(vm *engine.VM, this engine.Value, name string) *numberFormatData {
	nf, ok := engine.IntlData(this).(*numberFormatData)
	if !ok {
		vm.ThrowTypeError("Intl.NumberFormat.prototype." + name + " called on an object that is not an Intl.NumberFormat")
	}
	return nf
}

// partsArray returns the parts as an array of objects with the properties "type" and
// "value", and "source" for range parts.
func partsArray(vm *engine.VM, n int, part func(i int) (typ, value, source string)) engine.Value {
	keys := [...]engine.PropKey{
		engine.NamedKey(vm.Intern("type")),
		engine.NamedKey(vm.Intern("value")),
		engine.NamedKey(vm.Intern("source")),
	}
	vals := make([]engine.Value, n)
	var p engine.Pacer
	for i := range n {
		p.Step(vm)
		typ, value, source := part(i)
		props := [3]engine.Value{vm.NewString(typ), vm.NewString(value)}
		propertyCount := 2
		if source != "" {
			props[2] = vm.NewString(source)
			propertyCount++
		}
		vals[i] = engine.ObjectValue(vm.NewObjectFromProperties(keys[:propertyCount], props[:propertyCount]))
	}
	return vm.NewArrayOf(vals)
}

// formatRange checks and converts the arguments start and end of formatRange and
// formatRangeToParts, and implements PartitionNumberRangePattern for them.
func (nf *numberFormatData) formatRange(vm *engine.VM, args []engine.Value) []number.RangePart {
	start, end := arg(args, 0), arg(args, 1)
	if start.IsUndefined() || end.IsUndefined() {
		vm.ThrowTypeError("formatRange requires a start and an end")
	}
	x := toIntlMathematicalValue(vm, start)
	y := toIntlMathematicalValue(vm, end)
	if x.IsNaN() || y.IsNaN() {
		vm.ThrowRangeError("the start or the end of a range is NaN")
	}
	return nf.f.FormatRange(x, y, vm.Poll)
}

// initNumberFormat defines Intl.NumberFormat.
func (r *realm) initNumberFormat(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("NumberFormat", 0, r.newNumberFormat)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("NumberFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.numberFormatProto = proto
	r.numberFormatCtor = ctor

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nf := r.unwrapNumberFormat(vm, this, "resolvedOptions")
		return nf.resolvedOptions(vm)
	})
	getter(vm, proto, "format", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nf := r.unwrapNumberFormat(vm, this, "format")
		if nf.boundFormat == nil {
			nf.boundFormat = vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				x := toIntlMathematicalValue(vm, arg(args, 0))
				return formattedString(vm, nf.f.FormatToString(x, vm.Poll))
			})
		}
		return engine.ObjectValue(nf.boundFormat)
	})
	method(vm, proto, "formatRange", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nf := thisNumberFormat(vm, this, "formatRange")
		var b strings.Builder
		for _, p := range nf.formatRange(vm, args) {
			b.WriteString(p.Value)
		}
		checkStringLength(vm, b.Len())
		return vm.NewString(b.String())
	})
	method(vm, proto, "formatRangeToParts", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nf := thisNumberFormat(vm, this, "formatRangeToParts")
		parts := nf.formatRange(vm, args)
		return partsArray(vm, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, parts[i].Source
		})
	})
	method(vm, proto, "formatToParts", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nf := thisNumberFormat(vm, this, "formatToParts")
		parts := nf.f.Format(toIntlMathematicalValue(vm, arg(args, 0)), vm.Poll)
		return partsArray(vm, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, ""
		})
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.NumberFormat"), engine.FlagConfigurable)
}

// resolvedOptions implements Intl.NumberFormat.prototype.resolvedOptions after the check of
// the internal slot.
func (nf *numberFormatData) resolvedOptions(vm *engine.VM) engine.Value {
	o := &nf.opts
	out := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	set := func(name string, v engine.Value) {
		vm.CreateDataPropertyOrThrow(out, engine.NamedKey(vm.Intern(name)), v)
	}
	str := func(name, v string) {
		if v != "" {
			set(name, vm.NewString(v))
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
func formattedString(vm *engine.VM, s string) engine.Value {
	checkStringLength(vm, len(s))
	return vm.NewString(s)
}

// initToLocaleString replaces Number.prototype.toLocaleString and
// BigInt.prototype.toLocaleString with the versions of ECMA-402.
func initToLocaleString(vm *engine.VM) {
	format := func(vm *engine.VM, args []engine.Value, x number.Decimal) engine.Value {
		nf := createNumberFormat(vm, arg(args, 0), arg(args, 1))
		return formattedString(vm, nf.f.FormatToString(x, vm.Poll))
	}
	method(vm, vm.Intrinsic(engine.IntrinsicNumberPrototype), "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x, ok := engine.ThisNumberValue(this)
		if !ok {
			vm.ThrowTypeError("Number.prototype.toLocaleString requires that 'this' be a Number")
		}
		return format(vm, args, number.FromFloat(x))
	})
	method(vm, vm.Intrinsic(engine.IntrinsicBigIntPrototype), "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x, ok := engine.ThisBigIntValue(this)
		if !ok {
			vm.ThrowTypeError("BigInt.prototype.toLocaleString requires that 'this' be a BigInt")
		}
		return format(vm, args, number.FromInteger(vm.BigIntToString(x, 10)))
	})
}
