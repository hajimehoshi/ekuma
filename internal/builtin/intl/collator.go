// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/collate"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/unorm"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// collatorData holds the internal slots of an Intl.Collator object.
type collatorData struct {
	locale            string
	usage             string
	sensitivity       string
	ignorePunctuation bool
	collation         string
	numeric           bool
	caseFirst         string

	c            *collate.Collator
	boundCompare *engine.Object
}

// compare implements CompareStrings.
func (cd *collatorData) compare(cx *engine.ExecutionContext, x, y wtf8.String) int {
	return cd.c.ComparePolled(wtf8.ToValidUTF8(x), wtf8.ToValidUTF8(y), cx.Poll)
}

// sortCollationTypes returns the sort collations supported by l, with the default first.
func sortCollationTypes(l string) []string {
	t, ok := locale.Parse(l)
	if !ok {
		return []string{""}
	}
	return append([]string{""}, locale.CollationsOf(t)...)
}

// createCollator implements the steps of the Intl.Collator constructor after the prototype.
func createCollator(cx *engine.ExecutionContext, locales, opts engine.Value) *collatorData {
	requested := canonicalizeLocaleList(cx, locales)
	options := optionsObject(cx, opts)
	usage := getStringOption(cx, options, "usage", "sort", "sort", "search")
	bestFit := localeMatcher(cx, options)
	co, hasCo := typeOption(cx, options, "collation")
	var kn string
	hasKn := false
	if v := cx.Get(options, storage.NamedKey(cx.Intern("numeric"))); !v.IsUndefined() {
		kn, hasKn = "false", true
		if storage.ToBoolean(v) {
			kn = "true"
		}
	}
	kf, hasKf := getOption(cx, options, "caseFirst", "upper", "lower", "false")
	typ := "standard"
	coValues := sortCollationTypes
	if usage == "search" {
		typ = "search"
		coValues = func(string) []string { return []string{""} }
	}
	res := locale.ResolveLocale(requested, defaultLocale(cx), bestFit, []locale.ExtensionKey{
		{
			Key:       "co",
			Option:    co,
			HasOption: hasCo,
			Values:    coValues,
		},
		{
			Key:       "kf",
			Option:    kf,
			HasOption: hasKf,
			Values: func(l string) []string {
				d, _ := collate.Defaults(locale.DataLocale(l), typ)
				vs := []string{d}
				for _, v := range []string{"false", "lower", "upper"} {
					if v != d {
						vs = append(vs, v)
					}
				}
				return vs
			},
		},
		{
			Key:       "kn",
			Option:    kn,
			HasOption: hasKn,
			Values: func(string) []string {
				return []string{"false", "true"}
			},
		},
	})
	cd := &collatorData{
		locale:    res.Locale,
		usage:     usage,
		collation: res.Values[0],
		caseFirst: res.Values[1],
		numeric:   res.Values[2] == "true",
	}
	if cd.collation == "" {
		cd.collation = "default"
	}
	dataLocale := locale.DataLocale(res.DataLocale)
	cd.sensitivity = getStringOption(cx, options, "sensitivity", "variant", "base", "accent", "case", "variant")
	_, defaultIgnore := collate.Defaults(dataLocale, typ)
	cd.ignorePunctuation = defaultIgnore
	if v := cx.Get(options, storage.NamedKey(cx.Intern("ignorePunctuation"))); !v.IsUndefined() {
		cd.ignorePunctuation = storage.ToBoolean(v)
	}

	o := collate.Options{
		CaseFirst:         cd.caseFirst,
		Numeric:           cd.numeric,
		IgnorePunctuation: cd.ignorePunctuation,
	}
	switch cd.sensitivity {
	case "base":
		o.Strength = 1
	case "accent":
		o.Strength = 2
	case "case":
		o.Strength = 1
		o.CaseLevel = true
	case "variant":
		o.Strength = 3
	}
	if cd.collation != "default" {
		typ = cd.collation
	}
	cd.c = collate.New(dataLocale, typ, o)
	return cd
}

// newCollator implements the Intl.Collator constructor.
func newCollator(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicIntlCollatorPrototype)
	cd := createCollator(cx, arg(args, 0), arg(args, 1))
	return storage.ObjectValue(cx.NewIntlObject(proto, cd))
}

// thisCollator implements RequireInternalSlot for an Intl.Collator.
func thisCollator(cx *engine.ExecutionContext, this engine.Value, name string) *collatorData {
	cd, ok := storage.IntlData(this).(*collatorData)
	if !ok {
		cx.ThrowTypeError("Intl.Collator.prototype." + name + " called on an object that is not an Intl.Collator")
	}
	return cd
}

// collatorCompare is the code of the collator compare functions. Their state is a *collatorData.
func collatorCompare(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	cd := cx.ActiveNativeState().(*collatorData)
	x := cx.ToString(arg(args, 0))
	y := cx.ToString(arg(args, 1))
	return storage.Number(float64(cd.compare(cx, x, y)))
}

var (
	nativeNewCollator             = engine.RegisterNative(newCollator)
	nativeSupportedLocalesOf      = engine.RegisterNative(supportedLocalesOf)
	nativeCollatorCompareGetter   = engine.RegisterNative(collatorCompareGetter)
	nativeCollatorResolvedOptions = engine.RegisterNative(collatorResolvedOptions)
)

// initCollator defines Intl.Collator.
func initCollator(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := constructor(cx, ns, "Collator", 0, engine.IntrinsicIntlCollatorPrototype, nativeNewCollator)

	method(cx, ctor, "supportedLocalesOf", 1, nativeSupportedLocalesOf)
	getter(cx, proto, "compare", nativeCollatorCompareGetter)
	method(cx, proto, "resolvedOptions", 0, nativeCollatorResolvedOptions)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.Collator"), engine.FlagConfigurable)
}

var nativeCollatorCompare = engine.RegisterNative(collatorCompare)

func collatorCompareGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	cd := thisCollator(cx, this, "compare")
	if cd.boundCompare == nil {
		cd.boundCompare = cx.NewNativeWithState("", 2, nativeCollatorCompare, cd)
	}
	return storage.ObjectValue(cd.boundCompare)
}

func collatorResolvedOptions(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	cd := thisCollator(cx, this, "resolvedOptions")
	out := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	set := func(name string, v engine.Value) {
		cx.CreateDataPropertyOrThrow(out, storage.NamedKey(cx.Intern(wtf8.String(name))), v)
	}
	set("locale", cx.NewString(wtf8.String(cd.locale)))
	set("usage", cx.NewString(wtf8.String(cd.usage)))
	set("sensitivity", cx.NewString(wtf8.String(cd.sensitivity)))
	set("ignorePunctuation", storage.Bool(cd.ignorePunctuation))
	set("collation", cx.NewString(wtf8.String(cd.collation)))
	set("numeric", storage.Bool(cd.numeric))
	set("caseFirst", cx.NewString(wtf8.String(cd.caseFirst)))
	return storage.ObjectValue(out)
}

// localeCompareState is the state of String.prototype.localeCompare.
type localeCompareState struct {
	// collator is for calls without locales and options, created for the default locale locale.
	collator *collatorData
	locale   string
}

// defaultCollator returns the collator for calls without locales and options.
func (st *localeCompareState) defaultCollator(cx *engine.ExecutionContext) *collatorData {
	if l := defaultLocale(cx); st.collator == nil || st.locale != l {
		st.collator = createCollator(cx, storage.Undefined, storage.Undefined)
		st.locale = l
	}
	return st.collator
}

var nativeStringLocaleCompare = engine.RegisterNative(stringLocaleCompare)

// initLocaleCompare replaces String.prototype.localeCompare with the version of ECMA-402.
func initLocaleCompare(cx *engine.ExecutionContext) {
	f := cx.NewNativeWithState("localeCompare", 1, nativeStringLocaleCompare, &localeCompareState{})
	cx.Define(cx.Intrinsic(engine.IntrinsicStringPrototype), cx.Intern("localeCompare"), storage.ObjectValue(f), engine.FlagsHidden)
}

func stringLocaleCompare(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if this.IsNullish() {
		cx.ThrowTypeError("String.prototype.localeCompare called on null or undefined")
	}
	s := cx.ToString(this)
	that := cx.ToString(arg(args, 0))
	locales, options := arg(args, 1), arg(args, 2)
	var cd *collatorData
	if locales.IsUndefined() && options.IsUndefined() {
		cd = cx.ActiveNativeState().(*localeCompareState).defaultCollator(cx)
	} else {
		cd = createCollator(cx, locales, options)
	}
	return storage.Number(float64(cd.compare(cx, s, that)))
}

// caseLocales are the languages that have language-sensitive case mappings.
var caseLocales = []string{"az", "el", "hy", "lt", "tr"}

// initLocaleCase replaces String.prototype.toLocaleUpperCase and toLocaleLowerCase with the
// versions of ECMA-402.
func initLocaleCase(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicStringPrototype)
	define := func(name string, f engine.Native) {
		key := cx.Intern(wtf8.String(name))
		// The state of the method is that of the base, which maps the case without a language.
		fn := cx.NewNativeWithState(wtf8.String(name), 0, f, cx.Get(proto, storage.NamedKey(key)).Object())
		cx.Define(proto, key, storage.ObjectValue(fn), engine.FlagsHidden)
	}
	define("toLocaleUpperCase", nativeStringToLocaleUpperCase)
	define("toLocaleLowerCase", nativeStringToLocaleLowerCase)
}

var (
	nativeStringToLocaleUpperCase = engine.RegisterNative(stringLocaleCaseNative("toLocaleUpperCase", true))
	nativeStringToLocaleLowerCase = engine.RegisterNative(stringLocaleCaseNative("toLocaleLowerCase", false))
)

// stringLocaleCaseNative returns the method name of String.prototype, which maps a string to
// uppercase with upper and to lowercase otherwise.
//
//go:noinline
func stringLocaleCaseNative(name string, upper bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if this.IsNullish() {
			cx.ThrowTypeError("String.prototype." + name + " called on null or undefined")
		}
		value := cx.ToStringValue(this)
		// TransformCase.
		requested := canonicalizeLocaleList(cx, arg(args, 0))
		var tag string
		if len(requested) > 0 {
			tag = requested[0]
		} else {
			tag = defaultLocale(cx)
		}
		lang := ""
		if t, ok := locale.Parse(tag); ok {
			// LookupMatchingLocaleByPrefix over the languages, which have no other
			// subtags.
			if slices.Contains(caseLocales, t.Language) {
				lang = t.Language
			}
		}
		if lang == "" || !upper && (lang == "el" || lang == "hy") {
			if mapped, ok := cx.MapASCII(value, upper); ok {
				return mapped
			}
			return cx.Call(baseCaseMethod(cx), value)
		}
		s := value.Str()
		// Apply the language-specific rules before the default case mapping.
		switch {
		case lang == "el" && upper:
			s = wtf8.MapWellFormed(s, func(s string) string {
				return greekUpper(s, cx.Poll)
			})
		case lang == "hy" && upper:
			s = mapAtStarters(cx, s, func(s string, poll func()) string {
				return strings.ReplaceAll(s, "և", "ԵՎ")
			})
		case lang == "lt" && upper:
			s = mapAtStarters(cx, s, lithuanianUndot)
		case lang == "lt":
			s = mapAtStarters(cx, s, lithuanianDots)
		case (lang == "tr" || lang == "az") && upper:
			s = mapAtStarters(cx, s, func(s string, poll func()) string {
				return strings.ReplaceAll(s, "i", "\u0130")
			})
		case lang == "tr" || lang == "az":
			s = mapAtStarters(cx, s, turkicLowerI)
		}
		checkStringLength(cx, len(s))
		return cx.Call(baseCaseMethod(cx), cx.NewString(s))
	}
}

// baseCaseMethod returns the method of the base that the active case mapping method replaces.
func baseCaseMethod(cx *engine.ExecutionContext) *engine.Object {
	return cx.ActiveNativeState().(*engine.Object)
}

// caseChunk is the number of bytes of a string that a language-sensitive case mapping maps
// between two checks for an interruption.
const caseChunk = 1 << 16

// mapAtStarters applies f to the runs of s without lone surrogates, in parts that end before a
// character of the canonical combining class 0, checking for an interruption between the parts
// of a long string. f looks at no character beyond such a part, and calls poll now and then
// over a long part.
func mapAtStarters(cx *engine.ExecutionContext, s wtf8.String, f func(s string, poll func()) string) wtf8.String {
	return wtf8.MapWellFormed(s, func(s string) string {
		if len(s) <= caseChunk {
			return f(s, cx.Poll)
		}
		var b strings.Builder
		b.Grow(len(s))
		for len(s) > caseChunk {
			i := lastStarter(s[:caseChunk])
			if i <= 0 {
				i = nextStarter(s, caseChunk, cx.Poll)
			}
			b.WriteString(f(s[:i], cx.Poll))
			s = s[i:]
			cx.Poll()
		}
		b.WriteString(f(s, cx.Poll))
		return b.String()
	})
}

// lastStarter returns the offset of the last whole character of the combining class 0 in s
// other than its first, or -1. s may end inside a character.
func lastStarter(s string) int {
	for i := len(s) - 1; i > 0; i-- {
		if !utf8.RuneStart(s[i]) || !utf8.FullRuneInString(s[i:]) {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(s[i:]); unorm.CombiningClass(r) == 0 {
			return i
		}
	}
	return -1
}

// nextStarter returns the offset of the first character of the combining class 0 in s at or
// after the offset i, or len(s). poll is called now and then.
func nextStarter(s string, i int, poll func()) int {
	for k := 0; i < len(s); k++ {
		if k%pollRunes == 0 {
			poll()
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if utf8.RuneStart(s[i]) && unorm.CombiningClass(r) == 0 {
			return i
		}
		i += size
	}
	return len(s)
}

// pollRunes is the number of characters that a language-sensitive case mapping maps between
// two calls of its poll function.
const pollRunes = 1 << 14

// lithuanianDots applies the Lithuanian rules of SpecialCasing.txt for lowercasing that add a
// dot above to i and j, leaving the rest to the default lowercasing.
func lithuanianDots(s string, poll func()) string {
	var b strings.Builder
	b.Grow(len(s))
	k := 0
	for i, r := range s {
		if k++; k%pollRunes == 0 {
			poll()
		}
		switch r {
		case 'I', 'J', '\u012E':
			b.WriteRune(unicode.ToLower(r))
			if moreAbove(s[i+utf8.RuneLen(r):]) {
				b.WriteRune('\u0307')
			}
		case '\u00CC':
			b.WriteString("i\u0307\u0300")
		case '\u00CD':
			b.WriteString("i\u0307\u0301")
		case '\u0128':
			b.WriteString("i\u0307\u0303")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lithuanianUndot applies the Lithuanian rule of SpecialCasing.txt for uppercasing that
// removes a dot above after a soft-dotted character, leaving the rest to the default
// uppercasing.
func lithuanianUndot(s string, poll func()) string {
	var b strings.Builder
	b.Grow(len(s))
	afterSoftDotted := false
	k := 0
	for _, r := range s {
		if k++; k%pollRunes == 0 {
			poll()
		}
		c := unorm.CombiningClass(r)
		if r == '\u0307' && afterSoftDotted {
			afterSoftDotted = false
			continue
		}
		switch c {
		case 0:
			afterSoftDotted = unicode.Is(unicode.Soft_Dotted, r)
		case 230:
			afterSoftDotted = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// turkicLowerI applies the Turkish and Azerbaijani rules of SpecialCasing.txt for
// lowercasing the dotted and dotless i, leaving the rest to the default lowercasing.
func turkicLowerI(s string, poll func()) string {
	var b strings.Builder
	b.Grow(len(s))
	afterI := false
	k := 0
	for i, r := range s {
		if k++; k%pollRunes == 0 {
			poll()
		}
		c := unorm.CombiningClass(r)
		switch {
		case r == '\u0130':
			b.WriteByte('i')
		case r == 'I' && !beforeDot(s[i+1:]):
			b.WriteRune('\u0131')
		case r == '\u0307' && afterI:
			// The dot above of an I is removed.
		default:
			b.WriteRune(r)
		}
		switch c {
		case 0:
			afterI = r == 'I'
		case 230:
			afterI = false
		}
	}
	return b.String()
}

// moreAbove reports whether s starts with combining marks that include one of the combining
// class Above (230).
func moreAbove(s string) bool {
	for _, r := range s {
		switch unorm.CombiningClass(r) {
		case 0:
			return false
		case 230:
			return true
		}
	}
	return false
}

// beforeDot reports whether s starts with combining marks that include U+0307 COMBINING DOT
// ABOVE before any other of the combining class Above (230).
func beforeDot(s string) bool {
	for _, r := range s {
		if r == '\u0307' {
			return true
		}
		switch unorm.CombiningClass(r) {
		case 0, 230:
			return false
		}
	}
	return false
}

// joinLocaleStrings implements the toLocaleString methods of arrays for the first n elements of
// the object o.
func joinLocaleStrings(cx *engine.ExecutionContext, o *engine.Object, n int64, args []engine.Value) engine.Value {
	if !cx.EnterLocaleJoin(o) {
		return cx.NewString("")
	}
	defer cx.LeaveLocaleJoin()
	locales, options := arg(args, 0), arg(args, 1)
	var b wtf8.Buffer
	for k := range n {
		cx.PollAt(k)
		if k > 0 {
			b.WriteString(",")
		}
		e := cx.Get(o, cx.ToPropertyKey(storage.Number(float64(k))))
		if !e.IsNullish() {
			b.WriteString(cx.ToString(cx.CallValue(cx.GetV(e, cx.Key(engine.KeyToLocaleString)), e, locales, options)))
		}
		if b.Len() > engine.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
	}
	return cx.NewString(b.String())
}

var (
	nativeArrayToLocaleString      = engine.RegisterNative(arrayToLocaleString)
	nativeTypedArrayToLocaleString = engine.RegisterNative(typedArrayToLocaleString)
)

// initArrayToLocaleString replaces Array.prototype.toLocaleString and
// %TypedArray%.prototype.toLocaleString with the versions of ECMA-402, which pass the locales
// and the options to the elements.
func initArrayToLocaleString(cx *engine.ExecutionContext) {
	method(cx, cx.Intrinsic(engine.IntrinsicArrayPrototype), "toLocaleString", 0, nativeArrayToLocaleString)
	typedArrayProto := cx.ProtoOf(cx.Intrinsic(engine.IntrinsicInt8ArrayPrototype))
	method(cx, typedArrayProto, "toLocaleString", 0, nativeTypedArrayToLocaleString)
}

func arrayToLocaleString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := cx.ToObject(this)
	return joinLocaleStrings(cx, o, cx.LengthOfArrayLike(o), args)
}

func typedArrayToLocaleString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	// ValidateTypedArray.
	t := engine.TypedArrayData(this)
	if t == nil {
		cx.ThrowTypeError("%TypedArray%.prototype.toLocaleString: this is not a typed array")
	}
	n, ok := t.Length()
	if !ok {
		cx.ThrowTypeError("%TypedArray%.prototype.toLocaleString: the typed array is detached or out of bounds")
	}
	return joinLocaleStrings(cx, this.Object(), int64(n), args)
}
