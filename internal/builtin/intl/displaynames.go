// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/displaynames"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/localetag"
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
func (r *realm) newDisplayNames(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError("constructor Intl.DisplayNames requires 'new'")
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.displayNamesProto })
	requested := canonicalizeLocaleList(vm, arg(args, 0))
	// GetOptionsObject, with the options required.
	o := arg(args, 1)
	if !o.IsObject() {
		vm.ThrowTypeError("the options of Intl.DisplayNames must be an object")
	}
	options := o.Object()
	bestFit := localeMatcher(vm, options)
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, nil)
	dn := &displayNamesData{
		style: getStringOption(vm, options, "style", "long", "narrow", "short", "long"),
	}
	typ, ok := getOption(vm, options, "type", "language", "region", "script", "currency", "calendar", "dateTimeField")
	if !ok {
		vm.ThrowTypeError("the type option of Intl.DisplayNames is required")
	}
	dn.typ = typ
	dn.fallback = getStringOption(vm, options, "fallback", "code", "code", "none")
	dn.locale = res.Locale
	languageDisplay := getStringOption(vm, options, "languageDisplay", "dialect", "dialect", "standard")
	if typ == "language" {
		dn.languageDisplay = languageDisplay
	}
	dn.names = displaynames.New(res.DataLocale, dn.style != "long")
	return engine.ObjectValue(vm.NewIntlObject(proto, dn))
}

// thisDisplayNames implements RequireInternalSlot for an Intl.DisplayNames.
func thisDisplayNames(vm *engine.VM, this engine.Value, name string) *displayNamesData {
	dn, ok := engine.IntlData(this).(*displayNamesData)
	if !ok {
		vm.ThrowTypeError("Intl.DisplayNames.prototype." + name + " called on an object that is not an Intl.DisplayNames")
	}
	return dn
}

// of implements Intl.DisplayNames.prototype.of after the conversion of code to a String.
func (dn *displayNamesData) of(vm *engine.VM, code string) engine.Value {
	invalid := func() {
		vm.ThrowRangeError(vm.DisplayString(vm.NewString(wtf8.String(code))) + " is not a valid code for the type " + dn.typ)
	}
	var name string
	var ok bool
	switch dn.typ {
	case "language":
		t, valid := locale.Parse(code)
		if !valid || t.HasUnicode || t.HasTransformed || len(t.Others) > 0 || t.PrivateUse != "" {
			invalid()
		}
		c, _ := locale.Canonicalize(code, vm.Poll)
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
		return vm.NewString(wtf8.String(name))
	case dn.fallback == "code":
		return vm.NewString(wtf8.String(code))
	}
	return engine.Undefined
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

// initDisplayNames defines Intl.DisplayNames.
func (r *realm) initDisplayNames(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("DisplayNames", 2, r.newDisplayNames)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("DisplayNames"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.displayNamesProto = proto

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	method(vm, proto, "of", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		dn := thisDisplayNames(vm, this, "of")
		return dn.of(vm, wtf8.ToValidUTF8(vm.ToString(arg(args, 0))))
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		dn := thisDisplayNames(vm, this, "resolvedOptions")
		return resolvedOptions(vm, "locale", dn.locale, "style", dn.style, "type", dn.typ, "fallback", dn.fallback, "languageDisplay", dn.languageDisplay)
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.DisplayNames"), engine.FlagConfigurable)
}
