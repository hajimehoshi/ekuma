// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/displaynames"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// displayNamesData holds the internal slots of an Intl.DisplayNames object.
type displayNamesData struct {
	locale          string
	style           string
	typ             string
	fallback        string
	languageDisplay string
	names           displaynames.Names
}

// newDisplayNames implements the Intl.DisplayNames constructor.
func newDisplayNames(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.DisplayNames requires 'new'")
	}
	proto := cx.GetPrototypeFromConstructor(nt, storage.IntrinsicIntlDisplayNamesPrototype)
	requested := canonicalizeLocaleList(cx, arg(args, 0))
	// GetOptionsObject, with the options required.
	o := arg(args, 1)
	if !o.IsObject() {
		cx.ThrowTypeError("the options of Intl.DisplayNames must be an object")
	}
	options := o.Object()
	bestFit := localeMatcher(cx, options)
	res := locale.ResolveLocale(requested, defaultLocale(cx), bestFit, nil)
	dn := &displayNamesData{
		style: getStringOption(cx, options, "style", "long", "narrow", "short", "long"),
	}
	typ, ok := getOption(cx, options, "type", "language", "region", "script", "currency", "calendar", "dateTimeField")
	if !ok {
		cx.ThrowTypeError("the type option of Intl.DisplayNames is required")
	}
	dn.typ = typ
	dn.fallback = getStringOption(cx, options, "fallback", "code", "code", "none")
	dn.locale = res.Locale
	languageDisplay := getStringOption(cx, options, "languageDisplay", "dialect", "dialect", "standard")
	if typ == "language" {
		dn.languageDisplay = languageDisplay
	}
	dn.names = displaynames.New(res.DataLocale, dn.style != "long")
	return storage.ObjectValue(cx.NewIntlObject(proto, dn))
}

// thisDisplayNames implements RequireInternalSlot for an Intl.DisplayNames.
func thisDisplayNames(cx *engine.ExecutionContext, this storage.Value, name string) *displayNamesData {
	dn, ok := storage.IntlData(this).(*displayNamesData)
	if !ok {
		cx.ThrowTypeError("Intl.DisplayNames.prototype." + name + " called on an object that is not an Intl.DisplayNames")
	}
	return dn
}

// of implements Intl.DisplayNames.prototype.of after the conversion of code to a String.
func (dn *displayNamesData) of(cx *engine.ExecutionContext, code string) storage.Value {
	invalid := func() {
		cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(code))) + " is not a valid code for the type " + dn.typ)
	}
	var name string
	var ok bool
	switch dn.typ {
	case "language":
		t, valid := locale.Parse(code)
		if !valid || t.HasUnicode || t.HasTransformed || len(t.Others) > 0 || t.PrivateUse != "" {
			invalid()
		}
		c, _ := locale.Canonicalize(code, cx.Poll)
		ct, _ := locale.Parse(c)
		code = c
		name, ok = dn.names.Language(ct.LanguageID, dn.languageDisplay == "dialect")
	case "region":
		if !isASCII(code) || !localetag.IsRegionSubtag(strings.ToLower(code)) {
			invalid()
		}
		code = strings.ToUpper(code)
		name, ok = dn.names.Region(code)
	case "script":
		if !isASCII(code) || !localetag.IsScriptSubtag(strings.ToLower(code)) {
			invalid()
		}
		code = strings.ToUpper(code[:1]) + strings.ToLower(code[1:])
		name, ok = dn.names.Script(code)
	case "calendar":
		c := strings.ToLower(code)
		if !isASCII(code) || !localetag.IsType(c) {
			invalid()
		}
		code = c
		name, ok = dn.names.Calendar(code)
	case "dateTimeField":
		if !displaynames.IsDateTimeField(code) {
			invalid()
		}
		name, ok = dn.names.DateTimeField(code, dn.style)
	case "currency":
		if !isWellFormedCurrencyCode(code) {
			invalid()
		}
		code = strings.ToUpper(code)
		name, ok = dn.names.Currency(code)
	}
	switch {
	case ok:
		return cx.NewString(wtf8.String(name))
	case dn.fallback == "code":
		return cx.NewString(wtf8.String(code))
	}
	return storage.Undefined
}

// isASCII reports whether s has only ASCII characters.
func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

var (
	nativeNewDisplayNames             = engine.RegisterNative(newDisplayNames)
	nativeDisplayNamesOf              = engine.RegisterNative(displayNamesOf)
	nativeDisplayNamesResolvedOptions = engine.RegisterNative(displayNamesResolvedOptions)
)

// initDisplayNames defines Intl.DisplayNames.
func initDisplayNames(cx *engine.ExecutionContext, ns *storage.Object) {
	ctor, proto := constructor(cx, ns, "DisplayNames", 2, storage.IntrinsicIntlDisplayNamesPrototype, nativeNewDisplayNames)

	method(cx, ctor, "supportedLocalesOf", 1, nativeSupportedLocalesOf)
	method(cx, proto, "of", 1, nativeDisplayNamesOf)
	method(cx, proto, "resolvedOptions", 0, nativeDisplayNamesResolvedOptions)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Intl.DisplayNames"), storage.FlagConfigurable)
}

func displayNamesOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	dn := thisDisplayNames(cx, this, "of")
	return dn.of(cx, wtf8.ToValidUTF8(cx.ToString(arg(args, 0))))
}

func displayNamesResolvedOptions(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	dn := thisDisplayNames(cx, this, "resolvedOptions")
	return resolvedOptions(cx, "locale", dn.locale, "style", dn.style, "type", dn.typ, "fallback", dn.fallback, "languageDisplay", dn.languageDisplay)
}
