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
func thisLocale(cx *engine.ExecutionContext, this engine.Value, name string) *localeData {
	l, ok := engine.IntlData(this).(*localeData)
	if !ok {
		cx.ThrowTypeError("Intl.Locale.prototype." + name + " called on an object that is not an Intl.Locale")
	}
	return l
}

// asciiLower returns s in lower case, and reports whether s is ASCII.
func asciiLower(s string) (string, bool) {
	if !wtf8.IsASCII(wtf8.String(s)) {
		return "", false
	}
	return wtf8.ToLowerASCII(s), true
}

// subtagOption returns the option name of options in lower case if it is a subtag that valid
// accepts, or the fallback when the option is undefined. It throws a RangeError for any other
// value.
func subtagOption(cx *engine.ExecutionContext, options *engine.Object, name string, valid func(string) bool, fallback string) string {
	s, ok := getOption(cx, options, name)
	if !ok {
		return fallback
	}
	l, ok := asciiLower(s)
	if !ok || !valid(l) {
		cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for " + name)
	}
	return l
}

// validVariants reports whether s, in lower case, is a sequence of variant subtags without
// duplicates.
func validVariants(s string) bool {
	vs := strings.Split(s, "-")
	var seen map[string]struct{}
	for i, v := range vs {
		if !localetag.IsVariantSubtag(v) || localetag.HasDuplicate(&seen, vs[:i], v) {
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
func (r *realm) newLocale(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.Locale requires 'new'")
	}
	proto := protoFor(cx, nt, func(r *realm) *engine.Object { return r.localeProto })
	tagV := arg(args, 0)
	if !tagV.IsString() && !tagV.IsObject() {
		cx.ThrowTypeError("the tag of Intl.Locale must be a string or an object, not " + cx.DisplayString(tagV))
	}
	var tag string
	if l, ok := engine.IntlData(tagV).(*localeData); ok {
		tag = l.String()
	} else {
		tag = wtf8.ToValidUTF8(cx.ToString(tagV))
	}
	var options *engine.Object
	if o := arg(args, 1); o.IsUndefined() {
		options = cx.NewObject(nil)
	} else {
		options = cx.ToObject(o)
	}
	t, ok := locale.Parse(tag)
	if !ok {
		cx.ThrowRangeError("invalid language tag: " + cx.DisplayString(cx.NewString(wtf8.String(tag))))
	}
	t = t.CanonicalizePolled(cx.Poll)

	// UpdateLanguageId.
	t.Language = subtagOption(cx, options, "language", localetag.IsLanguageSubtag, t.Language)
	if s := subtagOption(cx, options, "script", localetag.IsScriptSubtag, strings.ToLower(t.Script)); s != "" {
		t.Script = strings.ToUpper(s[:1]) + s[1:]
	}
	t.Region = strings.ToUpper(subtagOption(cx, options, "region", localetag.IsRegionSubtag, t.Region))
	if vs := subtagOption(cx, options, "variants", validVariants, ""); vs != "" {
		t.Variants = strings.Split(vs, "-")
	}

	// The options of the relevant extension keys, in the order that they are read.
	var kws []locale.Keyword
	typeOption := func(name, key string, conv func(string) string) {
		s, ok := getOption(cx, options, name)
		if !ok {
			return
		}
		if conv != nil {
			s = conv(s)
		}
		l, ok := asciiLower(s)
		if !ok || !localetag.IsType(l) {
			cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for " + name)
		}
		kws = append(kws, locale.Keyword{
			Key:   key,
			Value: l,
		})
	}
	enumOption := func(name, key string, values ...string) {
		if s, ok := getOption(cx, options, name, values...); ok {
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
	if v := cx.Get(options, engine.NamedKey(cx.Intern("numeric"))); !v.IsUndefined() {
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
	return engine.ObjectValue(cx.NewIntlObject(proto, newLocaleData(t.CanonicalizePolled(cx.Poll))))
}

// initLocale defines Intl.Locale.
func (r *realm) initLocale(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := cx.NewNativeConstructor("Locale", 1, r.newLocale)
	cx.Define(ctor, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	cx.Define(r.ns, cx.Intern("Locale"), engine.ObjectValue(ctor), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.Locale"), engine.FlagConfigurable)
	r.localeProto = proto
	r.localeCtor = ctor

	newLocale := func(cx *engine.ExecutionContext, t locale.Tag) engine.Value {
		return engine.ObjectValue(cx.NewIntlObject(r.localeProto, newLocaleData(t)))
	}
	method(cx, proto, "maximize", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisLocale(cx, this, "maximize").tag
		if max, ok := locale.AddLikelySubtags(t.LanguageID); ok {
			t.LanguageID = max
		}
		return newLocale(cx, t)
	})
	method(cx, proto, "minimize", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisLocale(cx, this, "minimize").tag
		if min, ok := locale.RemoveLikelySubtags(t.LanguageID); ok {
			t.LanguageID = min
		}
		return newLocale(cx, t)
	})
	method(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.NewString(wtf8.String(thisLocale(cx, this, "toString").String()))
	})
	getter(cx, proto, "baseName", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.NewString(wtf8.String(thisLocale(cx, this, "baseName").tag.BaseName()))
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
		getter(cx, proto, k.name, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			v, ok := thisLocale(cx, this, k.name).tag.Keyword(k.key)
			return stringOrUndefined(cx, v, ok)
		})
	}
	getter(cx, proto, "numeric", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		v, ok := thisLocale(cx, this, "numeric").tag.Keyword("kn")
		return engine.Bool(ok && (v == "" || v == "true"))
	})
	getter(cx, proto, "language", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.NewString(wtf8.String(thisLocale(cx, this, "language").tag.Language))
	})
	getter(cx, proto, "script", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisLocale(cx, this, "script").tag.Script
		return stringOrUndefined(cx, s, s != "")
	})
	getter(cx, proto, "region", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisLocale(cx, this, "region").tag.Region
		return stringOrUndefined(cx, s, s != "")
	})
	getter(cx, proto, "variants", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		vs := thisLocale(cx, this, "variants").tag.Variants
		return stringOrUndefined(cx, strings.Join(vs, "-"), len(vs) > 0)
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
		method(cx, proto, m.name, 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return stringArray(cx, m.f(thisLocale(cx, this, m.name).tag))
		})
	}
	method(cx, proto, "getTimeZones", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		zones, ok := locale.TimeZonesOf(thisLocale(cx, this, "getTimeZones").tag)
		if !ok {
			return engine.Undefined
		}
		return stringArray(cx, zones)
	})
	method(cx, proto, "getTextInfo", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := locale.TextDirectionOf(thisLocale(cx, this, "getTextInfo").tag)
		info := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
		cx.CreateDataPropertyOrThrow(info, engine.NamedKey(cx.Intern("direction")), stringOrUndefined(cx, d, d != ""))
		return engine.ObjectValue(info)
	})
	method(cx, proto, "getWeekInfo", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		firstDay, weekend := locale.WeekInfoOf(thisLocale(cx, this, "getWeekInfo").tag)
		days := make([]engine.Value, len(weekend))
		for i, d := range weekend {
			days[i] = engine.Number(float64(d))
		}
		info := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
		cx.CreateDataPropertyOrThrow(info, engine.NamedKey(cx.Intern("firstDay")), engine.Number(float64(firstDay)))
		cx.CreateDataPropertyOrThrow(info, engine.NamedKey(cx.Intern("weekend")), cx.NewArrayOf(days))
		return engine.ObjectValue(info)
	})
}
