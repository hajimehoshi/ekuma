// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package number

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/intl/plural"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// Options holds the resolved options of an Intl.NumberFormat.
type Options struct {
	// Locale is the available locale whose data is used.
	Locale string
	// NumberingSystem is the numbering system, one with digits.
	NumberingSystem string
	// Style is "decimal", "percent", "currency" or "unit".
	Style string
	// Currency is the currency code in upper case for the style "currency".
	Currency string
	// CurrencyDisplay is "code", "symbol", "narrowSymbol" or "name".
	CurrencyDisplay string
	// CurrencySign is "standard" or "accounting".
	CurrencySign string
	// Unit is the unit identifier for the style "unit".
	Unit string
	// UnitDisplay is "short", "narrow" or "long".
	UnitDisplay string
	// Notation is "standard", "scientific", "engineering" or "compact".
	Notation string
	// CompactDisplay is "short" or "long".
	CompactDisplay string
	// UseGrouping is "always", "auto", "min2", or empty for no grouping.
	UseGrouping string
	// SignDisplay is "auto", "never", "always", "exceptZero" or "negative".
	SignDisplay string
	Digits
}

// Part is a part of a formatted number.
type Part struct {
	Type  string
	Value string
}

// Formatter formats numbers as an Intl.NumberFormat does.
type Formatter struct {
	o *Options
	d *localeData
	// pluralLocale is the locale of CLDR for plural rules.
	pluralLocale string

	decimal, group, minus, plus, percent, approximately, exponential, infinity, nan string
	digits                                                                          []string
	minGrouping                                                                     int
	// standard is the pattern of the style for standard notation.
	standard *pattern
	// compact holds the compact patterns, when the notation is compact.
	compact *compact
	// currency is the currency string for the style "currency" with a currency display other
	// than "name".
	currency string
}

// NewFormatter returns a formatter with the options o.
func NewFormatter(o *Options) *Formatter {
	d := newLocaleData(o.Locale, o.NumberingSystem)
	f := &Formatter{
		o:             o,
		d:             d,
		pluralLocale:  d.chain[0],
		decimal:       d.number("d"),
		group:         d.number("g"),
		minus:         d.number("-"),
		plus:          d.number("+"),
		percent:       d.number("p"),
		approximately: d.number("~"),
		exponential:   d.number("e"),
		infinity:      d.number("i"),
		nan:           d.number("n"),
	}
	if o.Style == "currency" {
		f.decimal = d.currencySymbol(o.Currency, "d")
		f.group = d.currencySymbol(o.Currency, "g")
	}
	f.digits = DigitsOf(o.NumberingSystem)
	switch o.UseGrouping {
	case "always":
		f.minGrouping = 1
	case "auto":
		f.minGrouping = d.minimumGroupingDigits()
	case "min2":
		f.minGrouping = max(2, d.minimumGroupingDigits())
	}
	var p string
	switch {
	case o.Style == "percent":
		p = d.number("P")
	case o.Style == "currency" && o.CurrencyDisplay != "name":
		// The locale's own pattern of the currency replaces both standard and accounting
		// patterns.
		if cp, ok := d.lookup(cldr.CurrencyData, o.Currency+"p"); ok {
			p = cp
		} else if o.CurrencySign == "accounting" {
			p = d.number("A")
		} else {
			p = d.number("C")
		}
	default:
		p = d.number("D")
	}
	f.standard = parsePattern(p)
	if o.Style == "currency" {
		switch o.CurrencyDisplay {
		case "code":
			f.currency = o.Currency
		case "symbol", "narrowSymbol":
			f.currency = o.Currency
			if s, ok := d.lookup(cldr.CurrencyData, o.Currency+"s"); ok {
				f.currency = s
			}
			if o.CurrencyDisplay == "narrowSymbol" {
				if s, ok := d.lookup(cldr.CurrencyData, o.Currency+"n"); ok {
					f.currency = s
				}
			}
		}
	}
	if o.Notation == "compact" {
		kind := "s"
		switch {
		case o.Style == "currency" && o.CurrencyDisplay != "name":
			// CLDR has compact currency patterns only in the short form.
			kind = "c"
		case o.CompactDisplay == "long":
			kind = "l"
		}
		c := d.compactOf(kind)
		f.compact = &c
	}
	return f
}

// formatted is a number formatted into the layers that a range collapses separately.
type formatted struct {
	// number are the parts of the number with its notation.
	number []Part
	// exponent are the parts of the exponent of scientific notation.
	exponent []Part
	// middle is the affix of the pattern: signs, currency, percent and compact symbols.
	middlePrefix, middleSuffix []Part
	// middleKey identifies the affix of the pattern regardless of plural forms.
	middleKey string
	// compactMag is the magnitude of the compact pattern, or -1.
	compactMag int
	sign       signType
	// outer is the unit or the currency name around the number.
	outerPrefix, outerSuffix []Part
	hasOuter                 bool
	// omitsNumber reports whether the pattern of the unit or the currency name has no
	// placeholder for the number, as the Arabic "سنة" for one year.
	omitsNumber bool
	// pluralString and pluralExponent select the plural form of the unit or currency name.
	pluralString   string
	pluralExponent int
	// compactString selects the plural form of the compact pattern.
	compactString string
}

// signOf returns the sign that the pattern shows for x, a rounded value.
func (f *Formatter) signOf(x Decimal) signType {
	negNonZero := x.neg && !x.nan && (x.inf || x.digits != "")
	posNonZero := !x.neg && !x.nan && (x.inf || x.digits != "")
	negZero := x.neg && x.IsZero()
	switch f.o.SignDisplay {
	case "never":
		return signNone
	case "always":
		if x.neg && !x.nan {
			return signMinus
		}
		return signPlus
	case "exceptZero":
		switch {
		case posNonZero:
			return signPlus
		case negNonZero:
			return signMinus
		}
		return signNone
	case "negative":
		if negNonZero {
			return signMinus
		}
		return signNone
	}
	if negNonZero || negZero {
		return signMinus
	}
	return signNone
}

// computeExponent implements ComputeExponent for the finite x. It also returns the magnitude
// of the compact pattern, or -1.
func (f *Formatter) computeExponent(x Decimal) (int, int) {
	if x.IsZero() {
		return 0, -1
	}
	mag := x.magnitude()
	exp, pmag := f.exponentForMagnitude(mag)
	_, r := f.o.Digits.FormatToString(x.scale(-exp))
	if r.IsZero() {
		return exp, pmag
	}
	if r.magnitude() == mag-exp {
		return exp, pmag
	}
	return f.exponentForMagnitude(mag + 1)
}

// exponentForMagnitude implements ComputeExponentForMagnitude.
func (f *Formatter) exponentForMagnitude(mag int) (int, int) {
	switch f.o.Notation {
	case "scientific":
		return mag, -1
	case "engineering":
		e := mag / 3
		if mag < 0 && mag%3 != 0 {
			e--
		}
		return e * 3, -1
	case "compact":
		return f.compact.exponentForMagnitude(mag)
	}
	return 0, -1
}

// format formats x into its layers. approximately adds the approximately sign. joined makes
// the integer digits and their group separators one part, for a formatted string. poll, which
// may be nil, is called now and then during a long number.
func (f *Formatter) format(x Decimal, approximately, joined bool, poll func()) *formatted {
	r := &formatted{
		compactMag: -1,
	}
	rounded := x
	var digits string
	switch {
	case x.nan:
		r.number = []Part{{
			Type:  "nan",
			Value: f.nan,
		}}
		r.compactString = "0"
		// Not-a-number has the pattern of positive zero.
		rounded = Decimal{}
	case x.inf:
		r.number = []Part{{
			Type:  "infinity",
			Value: f.infinity,
		}}
		r.compactString = "0"
	default:
		if f.o.Style == "percent" {
			x = x.scale(2)
		}
		exp, pmag := f.computeExponent(x)
		digits, rounded = f.o.Digits.FormatToString(x.scale(-exp))
		r.compactString = digits
		r.pluralString = digits
		if f.o.Notation == "compact" {
			r.pluralExponent = exp
			r.compactMag = pmag
		}
		if f.o.Notation == "scientific" || f.o.Notation == "engineering" {
			r.exponent = f.exponentParts(exp)
			// The plural form is that of the number that the notation stands for.
			r.pluralString = shiftPoint(digits, exp)
		}
	}
	r.sign = f.signOf(rounded)
	if hasNumber := f.setMiddle(r, approximately, ""); hasNumber && digits != "" {
		r.number = f.digitParts(digits, joined, poll)
	}
	f.setOuter(r, "")
	return r
}

// pluralOf returns the plural category of the formatted number s with the compact exponent
// e. An empty s, as for not-a-number and infinity, is of the category "other".
func (f *Formatter) pluralOf(s string, e int) string {
	if s == "" {
		return "other"
	}
	if e > 0 {
		// The operands are those of the number that the compact notation stands for.
		s = shiftPoint(s, e)
	}
	return plural.Select(f.pluralLocale, false, s, e)
}

// shiftPoint returns the decimal string s multiplied by 10**e.
func shiftPoint(s string, e int) string {
	i, frac, _ := strings.Cut(s, ".")
	if e >= 0 {
		n := min(e, len(frac))
		i += frac[:n] + strings.Repeat("0", e-n)
		if frac = frac[n:]; frac != "" {
			return i + "." + frac
		}
		return i
	}
	n := min(-e, len(i))
	frac = strings.Repeat("0", -e-n) + i[len(i)-n:] + frac
	if i = i[:len(i)-n]; i == "" {
		i = "0"
	}
	return i + "." + frac
}

// setMiddle sets the pattern affixes and reports whether the pattern has a number placeholder.
// A non-empty category overrides the plural category of the compact pattern.
func (f *Formatter) setMiddle(r *formatted, approximately bool, category string) bool {
	p := f.standard
	compactPattern := false
	if r.compactMag >= 0 {
		if category == "" {
			category = f.pluralOf(r.compactString, 0)
		}
		if s, ok := f.compact.pattern(r.compactMag, category); ok && !isOnlyZeros(s) {
			cp := parsePattern(s)
			switch {
			case f.o.Style == "currency" && f.o.CurrencyDisplay != "name" && !cp.positive.has(tokenCurrency):
				cp = f.withStandardAffix(cp, tokenCurrency)
			case f.o.Style == "percent" && !cp.positive.has(tokenPercent):
				cp = f.withStandardAffix(cp, tokenPercent)
			}
			p = cp
			compactPattern = true
		}
	}
	r.middlePrefix = f.affixParts(p.affix(true, r.sign, approximately), compactPattern)
	r.middleSuffix = f.affixParts(p.affix(false, r.sign, approximately), compactPattern)
	r.middleKey = strconv.Itoa(int(r.sign)) + "/" + strconv.Itoa(r.compactMag) + "/" + strconv.FormatBool(approximately)
	return p.hasNumber
}

// withStandardAffix adds the tokens of the kind of the standard pattern, with its literals,
// to the compact pattern cp.
func (f *Formatter) withStandardAffix(cp *pattern, kind tokenKind) *pattern {
	out := *cp
	if f.standard.positive.has(kind) {
		out.positive = withAffixOf(out.positive, f.standard.positive, kind)
		if out.hasNegative {
			out.negative = withAffixOf(out.negative, f.standard.positive, kind)
		}
	}
	return &out
}

// withAffixOf returns the subpattern s with the tokens of the kind and the literals of the
// subpattern std appended to its prefix and its suffix.
func withAffixOf(s, std subpattern, kind tokenKind) subpattern {
	for _, t := range std.prefix {
		if t.kind == kind || t.kind == tokenLiteral {
			s.prefix = append(s.prefix[:len(s.prefix):len(s.prefix)], t)
		}
	}
	for _, t := range std.suffix {
		if t.kind == kind || t.kind == tokenLiteral {
			s.suffix = append(s.suffix[:len(s.suffix):len(s.suffix)], t)
		}
	}
	return s
}

// affixParts converts the tokens of an affix to parts. In a compact pattern, literal text is
// the compact symbol.
func (f *Formatter) affixParts(tokens []token, compactPattern bool) []Part {
	var parts []Part
	for _, t := range tokens {
		switch t.kind {
		case tokenLiteral:
			if compactPattern {
				parts = append(parts, splitSpaces("compact", t.text)...)
			} else {
				parts = append(parts, Part{
					Type:  "literal",
					Value: t.text,
				})
			}
		case tokenMinus:
			parts = append(parts, Part{
				Type:  "minusSign",
				Value: f.minus,
			})
		case tokenPlus:
			parts = append(parts, Part{
				Type:  "plusSign",
				Value: f.plus,
			})
		case tokenApproximately:
			parts = append(parts, Part{
				Type:  "approximatelySign",
				Value: f.approximately,
			})
		case tokenPercent:
			if f.o.Style == "percent" {
				parts = append(parts, Part{
					Type:  "percentSign",
					Value: f.percent,
				})
			}
		case tokenPermille:
		case tokenCurrency:
			if f.o.Style == "currency" {
				parts = append(parts, Part{
					Type:  "currency",
					Value: f.currency,
				})
			}
		}
	}
	return parts
}

// splitSpaces returns the text s as a part of the type typ, with its leading and trailing
// white space as literal parts.
func splitSpaces(typ, s string) []Part {
	trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
	lead := s[:len(s)-len(trimmed)]
	core := strings.TrimRightFunc(trimmed, unicode.IsSpace)
	trail := trimmed[len(core):]
	var parts []Part
	for _, p := range []Part{{
		Type:  "literal",
		Value: lead,
	}, {
		Type:  typ,
		Value: core,
	}, {
		Type:  "literal",
		Value: trail,
	}} {
		if p.Value != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// setOuter sets the unit or the currency name around the number of r. A non-empty category
// overrides the plural category.
func (f *Formatter) setOuter(r *formatted, category string) {
	if f.o.Style != "unit" && !(f.o.Style == "currency" && f.o.CurrencyDisplay == "name") {
		return
	}
	if category == "" {
		category = f.pluralOf(r.pluralString, r.pluralExponent)
	}
	r.hasOuter = true
	if f.o.Style == "unit" {
		p := f.unitPattern(category)
		before, after, ok := strings.Cut(p, "{0}")
		r.omitsNumber = !ok
		r.outerPrefix = splitSpaces("unit", before)
		r.outerSuffix = splitSpaces("unit", after)
		return
	}
	p, ok := f.d.plural(cldr.NumberData, f.d.nu+".U", category)
	if !ok {
		p, ok = f.d.plural(cldr.NumberData, "latn.U", category)
	}
	if !ok {
		p = "{0} {1}"
	}
	name, ok := f.d.plural(cldr.CurrencyData, f.o.Currency, category)
	if !ok {
		if name, ok = f.d.lookup(cldr.CurrencyData, f.o.Currency+"d"); !ok {
			name = f.o.Currency
		}
	}
	before, after, ok := strings.Cut(p, "{0}")
	r.omitsNumber = !ok
	r.outerPrefix = currencyNameParts(before, name)
	r.outerSuffix = currencyNameParts(after, name)
}

// currencyNameParts returns the parts of the text s of a currency unit pattern, where "{1}"
// stands for the currency name.
func currencyNameParts(s, name string) []Part {
	var parts []Part
	before, after, ok := strings.Cut(s, "{1}")
	if before != "" {
		parts = append(parts, Part{
			Type:  "literal",
			Value: before,
		})
	}
	if ok {
		parts = append(parts, Part{
			Type:  "currency",
			Value: name,
		})
		if after != "" {
			parts = append(parts, Part{
				Type:  "literal",
				Value: after,
			})
		}
	}
	return parts
}

// unitPattern returns the pattern of the unit of f for the plural category, with "{0}" for
// the number.
func (f *Formatter) unitPattern(category string) string {
	w := "s"
	switch f.o.UnitDisplay {
	case "long":
		w = "l"
	case "narrow":
		w = "n"
	}
	u := f.o.Unit
	if p, ok := f.d.plural(cldr.UnitData, w+u+".", category); ok {
		return p
	}
	num, den, ok := strings.Cut(u, "-per-")
	if !ok {
		return "{0} " + u
	}
	p, ok := f.d.plural(cldr.UnitData, w+num+".", category)
	if !ok {
		p = "{0} " + num
	}
	if per, ok := f.d.lookup(cldr.UnitData, w+den+".p"); ok {
		return strings.Replace(per, "{0}", p, 1)
	}
	compound, ok := f.d.lookup(cldr.UnitData, w+"/")
	if !ok {
		compound = "{0}/{1}"
	}
	name, ok := f.d.plural(cldr.UnitData, w+den+".", "one")
	if !ok {
		name = den
	}
	name = strings.TrimSpace(strings.Replace(name, "{0}", "", 1))
	return strings.Replace(strings.Replace(compound, "{0}", p, 1), "{1}", name, 1)
}

// transliterate replaces the ASCII digits of s with those of the numbering system.
func (f *Formatter) transliterate(s string) string {
	return Transliterate(f.digits, s)
}

// DigitsOf returns the digits from zero to nine of the numbering system nu, or nil for "latn"
// and the numbering systems that have no digits.
func DigitsOf(nu string) []string {
	ds, ok := cldr.NumberingSystemDigits.Lookup(nu)
	if !ok || nu == "latn" {
		return nil
	}
	var digits []string
	for _, r := range ds {
		digits = append(digits, string(r))
	}
	return digits
}

// Transliterate replaces the ASCII digits of s with digits, the digits of a numbering system
// from [DigitsOf].
func Transliterate(digits []string, s string) string {
	if digits == nil {
		return s
	}
	var b strings.Builder
	for i := range len(s) {
		if c := s[i]; '0' <= c && c <= '9' {
			b.WriteString(digits[c-'0'])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// pollGroups is the number of digit groups between two calls of the poll function of a
// formatting.
const pollGroups = 1 << 14

// digitParts returns the parts of the formatted string s of ASCII digits and a full stop. joined
// makes the integer digits and their group separators one part. poll, which may be nil, is
// called now and then during a long number.
func (f *Formatter) digitParts(s string, joined bool, poll func()) []Part {
	intDigits, frac, hasFrac := strings.Cut(s, ".")
	var parts []Part
	p := f.standard
	n := len(intDigits)
	if f.o.UseGrouping != "" && p.primary > 0 && n-p.primary >= f.minGrouping {
		// The groups from the least significant one.
		var groups []string
		rest := intDigits
		size := p.primary
		for len(rest) > size {
			if poll != nil && len(groups)%pollGroups == pollGroups-1 {
				poll()
			}
			groups = append(groups, rest[len(rest)-size:])
			rest = rest[:len(rest)-size]
			size = p.secondary
		}
		groups = append(groups, rest)
		if joined {
			var b strings.Builder
			b.Grow(n + (len(groups)-1)*len(f.group))
			for i, group := range slices.Backward(groups) {
				b.WriteString(group)
				if i > 0 {
					b.WriteString(f.group)
				}
			}
			parts = append(parts, Part{
				Type:  "integer",
				Value: f.transliterate(b.String()),
			})
		} else {
			parts = slices.Grow(parts, 2*len(groups))
			for i, group := range slices.Backward(groups) {
				if poll != nil && i%pollGroups == 0 {
					poll()
				}
				parts = append(parts, Part{
					Type:  "integer",
					Value: f.transliterate(group),
				})
				if i > 0 {
					parts = append(parts, Part{
						Type:  "group",
						Value: f.group,
					})
				}
			}
		}
	} else {
		parts = append(parts, Part{
			Type:  "integer",
			Value: f.transliterate(intDigits),
		})
	}
	if hasFrac {
		parts = append(parts, Part{
			Type:  "decimal",
			Value: f.decimal,
		}, Part{
			Type:  "fraction",
			Value: f.transliterate(frac),
		})
	}
	return parts
}

// exponentParts returns the parts of the exponent e of scientific notation.
func (f *Formatter) exponentParts(e int) []Part {
	parts := []Part{{
		Type:  "exponentSeparator",
		Value: f.exponential,
	}}
	if e < 0 {
		parts = append(parts, Part{
			Type:  "exponentMinusSign",
			Value: f.minus,
		})
		e = -e
	}
	return append(parts, Part{
		Type:  "exponentInteger",
		Value: f.transliterate(strconv.Itoa(e)),
	})
}

// wrapMiddle returns body with the affix of the pattern, inserting the currency spacing.
func (f *Formatter) wrapMiddle(prefix, body, suffix []Part) []Part {
	before, after := f.currencySpacing(prefix, body, suffix)
	var out []Part
	out = append(out, prefix...)
	out = append(out, before...)
	out = append(out, body...)
	out = append(out, after...)
	return append(out, suffix...)
}

// currencySpacing returns the spacing to insert between the affixes of the pattern and body,
// when a currency of the affixes is next to a digit.
func (f *Formatter) currencySpacing(prefix, body, suffix []Part) (before, after []Part) {
	if len(body) == 0 {
		return nil, nil
	}
	if len(prefix) > 0 && prefix[len(prefix)-1].Type == "currency" {
		r, _ := utf8.DecodeLastRuneInString(prefix[len(prefix)-1].Value)
		d, _ := utf8.DecodeRuneInString(body[0].Value)
		if !isSymbolOrSeparator(r) && unicode.IsDigit(d) {
			before = []Part{{
				Type:  "literal",
				Value: f.d.number("B"),
			}}
		}
	}
	if len(suffix) > 0 && suffix[0].Type == "currency" {
		r, _ := utf8.DecodeRuneInString(suffix[0].Value)
		d, _ := utf8.DecodeLastRuneInString(body[len(body)-1].Value)
		if !isSymbolOrSeparator(r) && unicode.IsDigit(d) {
			after = []Part{{
				Type:  "literal",
				Value: f.d.number("b"),
			}}
		}
	}
	return before, after
}

func isSymbolOrSeparator(r rune) bool {
	return unicode.Is(unicode.S, r) || unicode.Is(unicode.Z, r)
}

// parts returns all the parts of r.
func (f *Formatter) parts(r *formatted) []Part {
	var body []Part
	if !r.omitsNumber {
		body = append(r.number[:len(r.number):len(r.number)], r.exponent...)
		body = f.wrapMiddle(r.middlePrefix, body, r.middleSuffix)
	}
	out := append(r.outerPrefix[:len(r.outerPrefix):len(r.outerPrefix)], body...)
	return mergeLiterals(append(out, r.outerSuffix...))
}

// mergeLiterals merges adjacent literal parts.
func mergeLiterals(parts []Part) []Part {
	var out []Part
	for _, p := range parts {
		if p.Value == "" {
			continue
		}
		if n := len(out); n > 0 && p.Type == "literal" && out[n-1].Type == "literal" {
			out[n-1].Value += p.Value
			continue
		}
		out = append(out, p)
	}
	return out
}

// Format implements PartitionNumberPattern. poll, which may be nil, is called now and then
// during a long number, and may stop the formatting by panicking.
func (f *Formatter) Format(x Decimal, poll func()) []Part {
	return f.parts(f.format(x, false, false, poll))
}

// FormatToString implements FormatNumeric: it returns the string of the parts of
// [Formatter.Format]. poll is as for [Formatter.Format].
func (f *Formatter) FormatToString(x Decimal, poll func()) string {
	return String(f.parts(f.format(x, false, true, poll)))
}

// String returns the concatenation of the values of parts.
func String(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Value)
	}
	return b.String()
}
