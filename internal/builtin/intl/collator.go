// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/collate"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/localetag"
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
func (cd *collatorData) compare(vm *engine.VM, x, y string) int {
	return cd.c.ComparePolled(wtf8.ToValidUTF8(x), wtf8.ToValidUTF8(y), vm.Poll)
}

// collationTypes returns the collation types of the locale l other than "standard" and
// "search", after the empty string for the default one.
func collationTypes(l string) []string {
	t, ok := locale.Parse(l)
	if !ok {
		return []string{""}
	}
	return append([]string{""}, locale.CollationsOf(t)...)
}

// createCollator implements the steps of the Intl.Collator constructor after the prototype.
func createCollator(vm *engine.VM, locales, opts engine.Value) *collatorData {
	requested := canonicalizeLocaleList(vm, locales)
	options := optionsObject(vm, opts)
	usage := getStringOption(vm, options, "usage", "sort", "sort", "search")
	bestFit := localeMatcher(vm, options)
	co, hasCo := getOption(vm, options, "collation")
	if hasCo && !localetag.IsType(co) {
		vm.ThrowRangeError("invalid collation: " + co)
	}
	var kn string
	hasKn := false
	if v := vm.Get(options, engine.NamedKey(vm.Intern("numeric"))); !v.IsUndefined() {
		kn, hasKn = "false", true
		if engine.ToBoolean(v) {
			kn = "true"
		}
	}
	kf, hasKf := getOption(vm, options, "caseFirst", "upper", "lower", "false")
	typ := "standard"
	if usage == "search" {
		typ = "search"
	}
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, []locale.ExtensionKey{
		{
			Key:       "co",
			Option:    co,
			HasOption: hasCo,
			Values:    collationTypes,
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
	cd.sensitivity = getStringOption(vm, options, "sensitivity", "variant", "base", "accent", "case", "variant")
	_, defaultIgnore := collate.Defaults(dataLocale, typ)
	cd.ignorePunctuation = defaultIgnore
	if v := vm.Get(options, engine.NamedKey(vm.Intern("ignorePunctuation"))); !v.IsUndefined() {
		cd.ignorePunctuation = engine.ToBoolean(v)
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
	if typ == "standard" && cd.collation != "default" {
		typ = cd.collation
	}
	cd.c = collate.New(dataLocale, typ, o)
	return cd
}

// newCollator implements the Intl.Collator constructor.
func (r *realm) newCollator(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		nt = r.collatorCtor
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.collatorProto })
	cd := createCollator(vm, arg(args, 0), arg(args, 1))
	return engine.ObjectValue(vm.NewIntlObject(proto, cd))
}

// thisCollator implements RequireInternalSlot for an Intl.Collator.
func thisCollator(vm *engine.VM, this engine.Value, name string) *collatorData {
	cd, ok := engine.IntlData(this).(*collatorData)
	if !ok {
		vm.ThrowTypeError("Intl.Collator.prototype." + name + " called on an object that is not an Intl.Collator")
	}
	return cd
}

// initCollator defines Intl.Collator.
func (r *realm) initCollator(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("Collator", 0, r.newCollator)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("Collator"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.collatorProto = proto
	r.collatorCtor = ctor

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	getter(vm, proto, "compare", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cd := thisCollator(vm, this, "compare")
		if cd.boundCompare == nil {
			cd.boundCompare = vm.NewNative("", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				x := vm.ToString(arg(args, 0))
				y := vm.ToString(arg(args, 1))
				return engine.Number(float64(cd.compare(vm, x, y)))
			})
		}
		return engine.ObjectValue(cd.boundCompare)
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cd := thisCollator(vm, this, "resolvedOptions")
		out := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
		set := func(name string, v engine.Value) {
			vm.CreateDataPropertyOrThrow(out, engine.NamedKey(vm.Intern(name)), v)
		}
		set("locale", vm.NewString(cd.locale))
		set("usage", vm.NewString(cd.usage))
		set("sensitivity", vm.NewString(cd.sensitivity))
		set("ignorePunctuation", engine.Bool(cd.ignorePunctuation))
		set("collation", vm.NewString(cd.collation))
		set("numeric", engine.Bool(cd.numeric))
		set("caseFirst", vm.NewString(cd.caseFirst))
		return engine.ObjectValue(out)
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.Collator"), engine.FlagConfigurable)
}

// initLocaleCompare replaces String.prototype.localeCompare with the version of ECMA-402.
func initLocaleCompare(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicStringPrototype)
	method(vm, proto, "localeCompare", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if this.IsNullish() {
			vm.ThrowTypeError("String.prototype.localeCompare called on null or undefined")
		}
		s := vm.ToString(this)
		that := vm.ToString(arg(args, 0))
		locales, options := arg(args, 1), arg(args, 2)
		var cd *collatorData
		if locales.IsUndefined() && options.IsUndefined() {
			r := vm.RealmExtension(vm.CurrentRealm()).(*realm)
			if l := defaultLocale(vm); r.defaultCollator == nil || r.defaultCollatorLocale != l {
				r.defaultCollator = createCollator(vm, engine.Undefined, engine.Undefined)
				r.defaultCollatorLocale = l
			}
			cd = r.defaultCollator
		} else {
			cd = createCollator(vm, locales, options)
		}
		return engine.Number(float64(cd.compare(vm, s, that)))
	})
}

// caseLocales are the languages that have language-sensitive case mappings.
var caseLocales = []string{"az", "el", "lt", "tr"}

// initLocaleCase replaces String.prototype.toLocaleUpperCase and toLocaleLowerCase with the
// versions of ECMA-402.
func initLocaleCase(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicStringPrototype)
	for _, m := range []struct {
		name  string
		upper bool
	}{
		{"toLocaleUpperCase", true},
		{"toLocaleLowerCase", false},
	} {
		// The methods of the base map the case without a language.
		base := vm.Get(proto, engine.NamedKey(vm.Intern(m.name)))
		method(vm, proto, m.name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			if this.IsNullish() {
				vm.ThrowTypeError("String.prototype." + m.name + " called on null or undefined")
			}
			value := vm.ToStringValue(this)
			// TransformCase.
			requested := canonicalizeLocaleList(vm, arg(args, 0))
			var tag string
			if len(requested) > 0 {
				tag = requested[0]
			} else {
				tag = defaultLocale(vm)
			}
			lang := ""
			if t, ok := locale.Parse(tag); ok {
				// LookupMatchingLocaleByPrefix over the languages, which have no other
				// subtags.
				if slices.Contains(caseLocales, t.Language) {
					lang = t.Language
				}
			}
			if lang == "" || lang == "el" && !m.upper {
				if mapped, ok := vm.MapASCII(value, m.upper); ok {
					return mapped
				}
				return vm.CallValue(base, value)
			}
			s := value.Str()
			// The rules of the language apply first, and then the base maps the case of the
			// result. Greek has rules only for uppercasing.
			switch {
			case lang == "el" && m.upper:
				s = mapAtStarters(vm, s, greekUpper)
				checkStringLength(vm, len(s))
				return vm.NewString(s)
			case lang == "lt" && m.upper:
				s = mapAtStarters(vm, s, lithuanianUndot)
			case lang == "lt":
				s = mapAtStarters(vm, s, lithuanianDots)
			case (lang == "tr" || lang == "az") && m.upper:
				s = mapAtStarters(vm, s, func(s string, poll func()) string {
					return strings.ReplaceAll(s, "i", "\u0130")
				})
			case lang == "tr" || lang == "az":
				s = mapAtStarters(vm, s, turkicLowerI)
			}
			checkStringLength(vm, len(s))
			return vm.CallValue(base, vm.NewString(s))
		})
	}
}

// caseChunk is the number of bytes of a string that a language-sensitive case mapping maps
// between two checks for an interruption.
const caseChunk = 1 << 16

// mapAtStarters applies f to the runs of s without lone surrogates, in parts that end before a
// character of the canonical combining class 0, checking for an interruption between the parts
// of a long string. f looks at no character beyond such a part, and calls poll now and then
// over a long part.
func mapAtStarters(vm *engine.VM, s string, f func(s string, poll func()) string) string {
	return wtf8.MapWellFormed(s, func(s string) string {
		if len(s) <= caseChunk {
			return f(s, vm.Poll)
		}
		var b strings.Builder
		b.Grow(len(s))
		for len(s) > caseChunk {
			i := lastStarter(s[:caseChunk])
			if i <= 0 {
				i = nextStarter(s, caseChunk, vm.Poll)
			}
			b.WriteString(f(s[:i], vm.Poll))
			s = s[i:]
			vm.Poll()
		}
		b.WriteString(f(s, vm.Poll))
		return b.String()
	})
}

// lastStarter returns the offset of the last character of the combining class 0 in s other
// than its first, or -1.
func lastStarter(s string) int {
	for i := len(s) - 1; i > 0; i-- {
		if !utf8.RuneStart(s[i]) {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(s[i:]); combiningClass(r) == 0 {
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
		if utf8.RuneStart(s[i]) && combiningClass(r) == 0 {
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

// greekUpper applies the Greek uppercasing of CLDR, which removes the diacritics of Greek
// letters.
func greekUpper(s string, poll func()) string {
	// cases keeps the diacritics of a letter whose uppercase in SpecialCasing.txt has several
	// characters, so the letters are decomposed first. A letter with an iota subscript is kept
	// for SpecialCasing.txt to turn the subscript into a capital iota.
	var b strings.Builder
	b.Grow(len(s))
	k := 0
	for _, r := range s {
		if k++; k%pollRunes == 0 {
			poll()
		}
		if unicode.Is(unicode.Greek, r) && unicode.IsLetter(r) {
			if d := norm.NFD.String(string(r)); !strings.ContainsRune(d, '\u0345') {
				b.WriteString(d)
				continue
			}
		}
		b.WriteRune(r)
	}
	return cases.Upper(language.Greek).String(b.String())
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
		c := combiningClass(r)
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
		c := combiningClass(r)
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
		switch combiningClass(r) {
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
		switch combiningClass(r) {
		case 0, 230:
			return false
		}
	}
	return false
}

// combiningClass returns the canonical combining class of r.
func combiningClass(r rune) uint8 {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], r)
	return norm.NFD.Properties(b[:n]).CCC()
}

// initArrayToLocaleString replaces Array.prototype.toLocaleString and
// %TypedArray%.prototype.toLocaleString with the versions of ECMA-402, which pass the locales
// and the options to the elements.
func initArrayToLocaleString(vm *engine.VM) {
	key := vm.Intern("toLocaleString")
	// joining are the arrays being converted, which an array that contains itself meets again.
	var joining []*engine.Object
	join := func(vm *engine.VM, o *engine.Object, n int64, args []engine.Value) engine.Value {
		if slices.Contains(joining, o) {
			return vm.NewString("")
		}
		joining = append(joining, o)
		defer func() {
			joining[len(joining)-1] = nil
			joining = joining[:len(joining)-1]
		}()
		locales, options := arg(args, 0), arg(args, 1)
		var b wtf8.Builder
		for k := range n {
			vm.PollAt(k)
			if k > 0 {
				b.WriteString(",")
			}
			e := vm.Get(o, vm.ToPropertyKey(engine.Number(float64(k))))
			if !e.IsNullish() {
				b.WriteString(vm.ToString(vm.CallValue(vm.GetV(e, key), e, locales, options)))
			}
			if b.Len() > engine.MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
		}
		return vm.NewString(b.String())
	}
	method(vm, vm.Intrinsic(engine.IntrinsicArrayPrototype), "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		return join(vm, o, vm.LengthOfArrayLike(o), args)
	})
	typedArrayProto := vm.ProtoOf(vm.Intrinsic(engine.IntrinsicInt8ArrayPrototype))
	method(vm, typedArrayProto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		// ValidateTypedArray.
		t := engine.TypedArrayData(this)
		if t == nil {
			vm.ThrowTypeError("%TypedArray%.prototype.toLocaleString: this is not a typed array")
		}
		n, ok := t.Length()
		if !ok {
			vm.ThrowTypeError("%TypedArray%.prototype.toLocaleString: the typed array is detached or out of bounds")
		}
		return join(vm, this.Object(), int64(n), args)
	})
}
