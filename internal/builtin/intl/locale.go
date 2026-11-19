// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// localeData holds the internal slots of an Intl.Locale object.
type localeData struct {
	// tag is the [[Locale]] of the object in canonical form.
	tag locale.Tag
	// str is tag as a string.
	str string
}

func newLocaleData(t locale.Tag) *localeData {
	return &localeData{
		tag: t,
		str: t.String(),
	}
}

// String returns the [[Locale]] of l.
func (l *localeData) String() string {
	return l.str
}

// thisLocale returns the internal slots of the Intl.Locale object this, or throws a TypeError
// naming the method.
func thisLocale(vm *engine.VM, this engine.Value, name string) *localeData {
	l, ok := engine.IntlData(this).(*localeData)
	if !ok {
		vm.ThrowTypeError("Intl.Locale.prototype." + name + " called on an object that is not an Intl.Locale")
	}
	return l
}

// asciiLower returns s in lower case, and reports whether s is ASCII.
func asciiLower(s string) (string, bool) {
	if !wtf8.IsASCII(s) {
		return "", false
	}
	return xstrings.ToLowerASCII(s), true
}

// subtagOption returns the option name of options in lower case if it is a subtag that valid
// accepts, or the fallback when the option is undefined. It throws a RangeError for any other
// value.
func subtagOption(vm *engine.VM, options *engine.Object, name string, valid func(string) bool, fallback string) string {
	s, ok := getOption(vm, options, name)
	if !ok {
		return fallback
	}
	l, ok := asciiLower(s)
	if !ok || !valid(l) {
		vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for " + name)
	}
	return l
}

// validVariants reports whether s, in lower case, is a sequence of variant subtags without
// duplicates.
func validVariants(s string) bool {
	vs := strings.Split(s, "-")
	for i, v := range vs {
		if !localetag.IsVariantSubtag(v) || slices.Contains(vs[:i], v) {
			return false
		}
	}
	return true
}

// weekdayToString implements WeekdayToString.
func weekdayToString(s string) string {
	switch s {
	case "0", "7":
		return "sun"
	case "1":
		return "mon"
	case "2":
		return "tue"
	case "3":
		return "wed"
	case "4":
		return "thu"
	case "5":
		return "fri"
	case "6":
		return "sat"
	}
	return s
}

// newLocale implements the Intl.Locale constructor.
func (r *realm) newLocale(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError("constructor Intl.Locale requires 'new'")
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.localeProto })
	tagV := arg(args, 0)
	if !tagV.IsString() && !tagV.IsObject() {
		vm.ThrowTypeError("the tag of Intl.Locale must be a string or an object, not " + vm.DisplayString(tagV))
	}
	var tag string
	if l, ok := engine.IntlData(tagV).(*localeData); ok {
		tag = l.String()
	} else {
		tag = vm.ToString(tagV)
	}
	var options *engine.Object
	if o := arg(args, 1); o.IsUndefined() {
		options = vm.NewObject(nil)
	} else {
		options = vm.ToObject(o)
	}
	t, ok := locale.Parse(tag)
	if !ok {
		vm.ThrowRangeError("invalid language tag: " + tag)
	}
	t = t.Canonicalize()

	// UpdateLanguageId.
	t.Language = subtagOption(vm, options, "language", localetag.IsLanguageSubtag, t.Language)
	if s := subtagOption(vm, options, "script", localetag.IsScriptSubtag, strings.ToLower(t.Script)); s != "" {
		t.Script = strings.ToUpper(s[:1]) + s[1:]
	}
	t.Region = strings.ToUpper(subtagOption(vm, options, "region", localetag.IsRegionSubtag, t.Region))
	if vs := subtagOption(vm, options, "variants", validVariants, ""); vs != "" {
		t.Variants = strings.Split(vs, "-")
	}

	// The options of the relevant extension keys, in the order that they are read.
	var kws []locale.Keyword
	typeOption := func(name, key string, conv func(string) string) {
		s, ok := getOption(vm, options, name)
		if !ok {
			return
		}
		if conv != nil {
			s = conv(s)
		}
		l, ok := asciiLower(s)
		if !ok || !localetag.IsType(l) {
			vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for " + name)
		}
		kws = append(kws, locale.Keyword{
			Key:   key,
			Value: l,
		})
	}
	enumOption := func(name, key string, values ...string) {
		if s, ok := getOption(vm, options, name, values...); ok {
			kws = append(kws, locale.Keyword{
				Key:   key,
				Value: s,
			})
		}
	}
	typeOption("calendar", "ca", nil)
	typeOption("collation", "co", nil)
	typeOption("firstDayOfWeek", "fw", weekdayToString)
	enumOption("hourCycle", "hc", "h11", "h12", "h23", "h24")
	enumOption("caseFirst", "kf", "upper", "lower", "false")
	if v := vm.Get(options, engine.NamedKey(vm.Intern("numeric"))); !v.IsUndefined() {
		s := "false"
		if engine.ToBoolean(v) {
			s = "true"
		}
		kws = append(kws, locale.Keyword{
			Key:   "kn",
			Value: s,
		})
	}
	typeOption("numberingSystem", "nu", nil)

	// MakeLocaleRecord.
	if len(kws) > 0 {
		t.HasUnicode = true
		t.Keywords = slices.Clone(t.Keywords)
		for _, k := range kws {
			if i := slices.IndexFunc(t.Keywords, func(e locale.Keyword) bool { return e.Key == k.Key }); i >= 0 {
				t.Keywords[i] = k
			} else {
				t.Keywords = append(t.Keywords, k)
			}
		}
	}
	return engine.ObjectValue(vm.NewIntlObject(proto, newLocaleData(t.Canonicalize())))
}

// initLocale defines Intl.Locale.
func (r *realm) initLocale(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("Locale", 1, r.newLocale)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("Locale"), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.Locale"), engine.FlagConfigurable)
	r.localeProto = proto
	r.localeCtor = ctor

	newLocale := func(t locale.Tag) engine.Value {
		return engine.ObjectValue(vm.NewIntlObject(r.localeProto, newLocaleData(t)))
	}
	method(vm, proto, "maximize", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisLocale(vm, this, "maximize").tag
		if max, ok := locale.AddLikelySubtags(t.LanguageID); ok {
			t.LanguageID = max
		}
		return newLocale(t)
	})
	method(vm, proto, "minimize", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisLocale(vm, this, "minimize").tag
		if min, ok := locale.RemoveLikelySubtags(t.LanguageID); ok {
			t.LanguageID = min
		}
		return newLocale(t)
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(thisLocale(vm, this, "toString").String())
	})
	getter(vm, proto, "baseName", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(thisLocale(vm, this, "baseName").tag.BaseName())
	})
	for _, k := range []struct {
		name, key string
	}{
		{"calendar", "ca"},
		{"caseFirst", "kf"},
		{"collation", "co"},
		{"firstDayOfWeek", "fw"},
		{"hourCycle", "hc"},
		{"numberingSystem", "nu"},
	} {
		getter(vm, proto, k.name, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			v, ok := thisLocale(vm, this, k.name).tag.Keyword(k.key)
			return stringOrUndefined(vm, v, ok)
		})
	}
	getter(vm, proto, "numeric", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v, ok := thisLocale(vm, this, "numeric").tag.Keyword("kn")
		return engine.Bool(ok && (v == "" || v == "true"))
	})
	getter(vm, proto, "language", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(thisLocale(vm, this, "language").tag.Language)
	})
	getter(vm, proto, "script", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisLocale(vm, this, "script").tag.Script
		return stringOrUndefined(vm, s, s != "")
	})
	getter(vm, proto, "region", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisLocale(vm, this, "region").tag.Region
		return stringOrUndefined(vm, s, s != "")
	})
	getter(vm, proto, "variants", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vs := thisLocale(vm, this, "variants").tag.Variants
		return stringOrUndefined(vm, strings.Join(vs, "-"), len(vs) > 0)
	})
	for _, m := range []struct {
		name string
		f    func(locale.Tag) []string
	}{
		{"getCalendars", locale.CalendarsOf},
		{"getCollations", locale.CollationsOf},
		{"getHourCycles", locale.HourCyclesOf},
		{"getNumberingSystems", locale.NumberingSystemsOf},
	} {
		method(vm, proto, m.name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return stringArray(vm, m.f(thisLocale(vm, this, m.name).tag))
		})
	}
	method(vm, proto, "getTimeZones", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		zones, ok := locale.TimeZonesOf(thisLocale(vm, this, "getTimeZones").tag)
		if !ok {
			return engine.Undefined
		}
		return stringArray(vm, zones)
	})
	method(vm, proto, "getTextInfo", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		d := locale.TextDirectionOf(thisLocale(vm, this, "getTextInfo").tag)
		info := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
		vm.CreateDataPropertyOrThrow(info, engine.NamedKey(vm.Intern("direction")), stringOrUndefined(vm, d, d != ""))
		return engine.ObjectValue(info)
	})
	method(vm, proto, "getWeekInfo", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		firstDay, weekend := locale.WeekInfoOf(thisLocale(vm, this, "getWeekInfo").tag)
		days := make([]engine.Value, len(weekend))
		for i, d := range weekend {
			days[i] = engine.Number(float64(d))
		}
		info := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
		vm.CreateDataPropertyOrThrow(info, engine.NamedKey(vm.Intern("firstDay")), engine.Number(float64(firstDay)))
		vm.CreateDataPropertyOrThrow(info, engine.NamedKey(vm.Intern("weekend")), vm.NewArrayOf(days))
		return engine.ObjectValue(info)
	})
}
