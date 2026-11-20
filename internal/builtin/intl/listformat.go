// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/list"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// listFormatData holds the internal slots of an Intl.ListFormat object.
type listFormatData struct {
	locale string
	typ    string
	style  string
	f      *list.Formatter
}

// getOptionsObject implements GetOptionsObject.
func getOptionsObject(vm *engine.VM, v engine.Value, name string) *engine.Object {
	switch {
	case v.IsUndefined():
		return vm.NewObject(nil)
	case v.IsObject():
		return v.Object()
	}
	vm.ThrowTypeError("the options of " + name + " must be an object")
	return nil
}

// newListFormat implements the Intl.ListFormat constructor.
func (r *realm) newListFormat(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError("constructor Intl.ListFormat requires 'new'")
	}
	proto := protoFor(vm, nt, func(r *realm) *engine.Object { return r.listFormatProto })
	requested := canonicalizeLocaleList(vm, arg(args, 0))
	options := getOptionsObject(vm, arg(args, 1), "Intl.ListFormat")
	bestFit := localeMatcher(vm, options)
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, nil)
	lf := &listFormatData{
		locale: res.Locale,
		typ:    getStringOption(vm, options, "type", "conjunction", "conjunction", "disjunction", "unit"),
		style:  getStringOption(vm, options, "style", "long", "long", "short", "narrow"),
	}
	lf.f = list.NewFormatter(res.DataLocale, lf.typ, lf.style)
	return engine.ObjectValue(vm.NewIntlObject(proto, lf))
}

// thisListFormat implements RequireInternalSlot for an Intl.ListFormat.
func thisListFormat(vm *engine.VM, this engine.Value, name string) *listFormatData {
	lf, ok := engine.IntlData(this).(*listFormatData)
	if !ok {
		vm.ThrowTypeError("Intl.ListFormat.prototype." + name + " called on an object that is not an Intl.ListFormat")
	}
	return lf
}

// stringListFromIterable implements StringListFromIterable.
func stringListFromIterable(vm *engine.VM, v engine.Value) []string {
	if v.IsUndefined() {
		return nil
	}
	it, next := vm.GetIterator(v)
	var out []string
	for {
		e, ok := vm.IteratorStepValue(it, next)
		if !ok {
			return out
		}
		if !e.IsString() {
			vm.IfAbruptCloseIterator(it, func() {
				vm.ThrowTypeError("an element of the list is not a string: " + vm.DisplayString(e))
			})
		}
		// The formatter copies the elements into its parts as they are, lone surrogates included.
		out = append(out, string(vm.ToString(e)))
	}
}

// initListFormat defines Intl.ListFormat.
func (r *realm) initListFormat(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("ListFormat", 0, r.newListFormat)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("ListFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.listFormatProto = proto

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	method(vm, proto, "format", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		lf := thisListFormat(vm, this, "format")
		parts := lf.f.Format(stringListFromIterable(vm, arg(args, 0)))
		var n int
		var previous wtf8.String
		for _, p := range parts {
			v := wtf8.String(p.Value)
			if v == "" {
				continue
			}
			n += len(v)
			if wtf8.JoinsPair(previous, v) {
				// Two three-byte surrogates form a four-byte code point.
				n -= 2
			}
			checkStringLength(vm, n)
			previous = v
		}
		var b wtf8.Builder
		b.Grow(n)
		for _, p := range parts {
			b.WriteString(wtf8.String(p.Value))
		}
		return vm.NewString(b.String())
	})
	method(vm, proto, "formatToParts", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		lf := thisListFormat(vm, this, "formatToParts")
		parts := lf.f.Format(stringListFromIterable(vm, arg(args, 0)))
		return partsArray(vm, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, ""
		})
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		lf := thisListFormat(vm, this, "resolvedOptions")
		return resolvedOptions(vm, "locale", lf.locale, "type", lf.typ, "style", lf.style)
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.ListFormat"), engine.FlagConfigurable)
}

// resolvedOptions returns a new object with the string properties of the pairs of names and
// values kv, in order, leaving out the empty values.
func resolvedOptions(vm *engine.VM, kv ...string) engine.Value {
	var keyStorage [32]engine.PropKey
	var valueStorage [32]engine.Value
	keys := keyStorage[:0]
	vals := valueStorage[:0]
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] != "" {
			keys = append(keys, engine.NamedKey(vm.Intern(wtf8.String(kv[i]))))
			vals = append(vals, vm.NewString(wtf8.String(kv[i+1])))
		}
	}
	return engine.ObjectValue(vm.NewObjectFromProperties(keys, vals))
}
