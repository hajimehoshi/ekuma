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
	if hasCo && !locale.IsType(co) {
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
	res := locale.ResolveLocale(requested, bestFit, []locale.ExtensionKey{
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
			if r.defaultCollator == nil {
				r.defaultCollator = createCollator(vm, engine.Undefined, engine.Undefined)
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
			s := vm.ToString(this)
			// TransformCase.
			requested := canonicalizeLocaleList(vm, arg(args, 0))
			var tag string
			if len(requested) > 0 {
				tag = requested[0]
			} else {
				tag = locale.Default()
			}
			lang := ""
			if t, ok := locale.Parse(tag); ok {
				// LookupMatchingLocaleByPrefix over the languages, which have no other
				// subtags.
				if slices.Contains(caseLocales, t.Language) {
					lang = t.Language
				}
			}
			// The rules of the language apply first, and then the base maps the case of the
			// result. Greek has rules only for uppercasing.
			switch {
			case lang == "el" && m.upper:
				return vm.NewString(mapWellFormed(s, greekUpper))
			case lang == "lt" && m.upper:
				s = mapWellFormed(s, lithuanianUndot)
			case lang == "lt":
				s = mapWellFormed(s, lithuanianDots)
			case (lang == "tr" || lang == "az") && m.upper:
				s = strings.ReplaceAll(s, "i", "\u0130")
			case lang == "tr" || lang == "az":
				s = mapWellFormed(s, turkicLowerI)
			}
			return vm.CallValue(base, vm.NewString(s))
		})
	}
}

// mapWellFormed applies f to the parts of s between lone surrogates.
func mapWellFormed(s string, f func(string) string) string {
	var b wtf8.Builder
	for len(s) > 0 {
		i := 0
		for i < len(s) {
			r, size := wtf8.DecodeRune(s[i:])
			if r >= 0xD800 && r <= 0xDFFF {
				break
			}
			i += size
		}
		b.WriteString(f(s[:i]))
		s = s[i:]
		if len(s) > 0 {
			r, size := wtf8.DecodeRune(s)
			b.WriteRune(r)
			s = s[size:]
		}
	}
	return b.String()
}

// lithuanianDots applies the Lithuanian rules of SpecialCasing.txt for lowercasing that add a
// dot above to i and j, leaving the rest to the default lowercasing.
func lithuanianDots(s string) string {
	rs := []rune(s)
	var out []rune
	for i, r := range rs {
		switch r {
		case 'I', 'J', '\u012E':
			out = append(out, unicode.ToLower(r))
			if moreAbove(rs[i+1:]) {
				out = append(out, '\u0307')
			}
		case '\u00CC':
			out = append(out, 'i', '\u0307', '\u0300')
		case '\u00CD':
			out = append(out, 'i', '\u0307', '\u0301')
		case '\u0128':
			out = append(out, 'i', '\u0307', '\u0303')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// greekUpper applies the Greek uppercasing of CLDR, which removes the diacritics of Greek
// letters.
func greekUpper(s string) string {
	// cases keeps the diacritics of a letter whose uppercase in SpecialCasing.txt has several
	// characters, so the letters are decomposed first. A letter with an iota subscript is kept
	// for SpecialCasing.txt to turn the subscript into a capital iota.
	var b strings.Builder
	for _, r := range s {
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
func lithuanianUndot(s string) string {
	var out []rune
	afterSoftDotted := false
	for _, r := range s {
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
		out = append(out, r)
	}
	return string(out)
}

// turkicLowerI applies the Turkish and Azerbaijani rules of SpecialCasing.txt for
// lowercasing the dotted and dotless i, leaving the rest to the default lowercasing.
func turkicLowerI(s string) string {
	rs := []rune(s)
	var out []rune
	afterI := false
	for i, r := range rs {
		c := combiningClass(r)
		switch {
		case r == '\u0130':
			out = append(out, 'i')
		case r == 'I' && !beforeDot(rs[i+1:]):
			out = append(out, '\u0131')
		case r == '\u0307' && afterI:
			// The dot above of an I is removed.
		default:
			out = append(out, r)
		}
		switch c {
		case 0:
			afterI = r == 'I'
		case 230:
			afterI = false
		}
	}
	return string(out)
}

// moreAbove reports whether rs starts with combining marks that include one of the combining
// class Above (230).
func moreAbove(rs []rune) bool {
	for _, r := range rs {
		switch combiningClass(r) {
		case 0:
			return false
		case 230:
			return true
		}
	}
	return false
}

// beforeDot reports whether rs starts with combining marks that include U+0307 COMBINING DOT
// ABOVE before any other of the combining class Above (230).
func beforeDot(rs []rune) bool {
	for _, r := range rs {
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
