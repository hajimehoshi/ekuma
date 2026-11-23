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
func getOptionsObject(cx *engine.ExecutionContext, v engine.Value, name string) *engine.Object {
	switch {
	case v.IsUndefined():
		return cx.NewObject(nil)
	case v.IsObject():
		return v.Object()
	}
	cx.ThrowTypeError("the options of " + name + " must be an object")
	return nil
}

// newListFormat implements the Intl.ListFormat constructor.
func (r *realm) newListFormat(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Intl.ListFormat requires 'new'")
	}
	proto := protoFor(cx, nt, func(r *realm) *engine.Object { return r.listFormatProto })
	requested := canonicalizeLocaleList(cx, arg(args, 0))
	options := getOptionsObject(cx, arg(args, 1), "Intl.ListFormat")
	bestFit := localeMatcher(cx, options)
	res := locale.ResolveLocale(requested, defaultLocale(cx), bestFit, nil)
	lf := &listFormatData{
		locale: res.Locale,
		typ:    getStringOption(cx, options, "type", "conjunction", "conjunction", "disjunction", "unit"),
		style:  getStringOption(cx, options, "style", "long", "long", "short", "narrow"),
	}
	lf.f = list.NewFormatter(res.DataLocale, lf.typ, lf.style)
	return engine.ObjectValue(cx.NewIntlObject(proto, lf))
}

// thisListFormat implements RequireInternalSlot for an Intl.ListFormat.
func thisListFormat(cx *engine.ExecutionContext, this engine.Value, name string) *listFormatData {
	lf, ok := engine.IntlData(this).(*listFormatData)
	if !ok {
		cx.ThrowTypeError("Intl.ListFormat.prototype." + name + " called on an object that is not an Intl.ListFormat")
	}
	return lf
}

// stringListFromIterable implements StringListFromIterable.
func stringListFromIterable(cx *engine.ExecutionContext, v engine.Value) []string {
	if v.IsUndefined() {
		return nil
	}
	it, next := cx.GetIterator(v)
	var out []string
	for {
		e, ok := cx.IteratorStepValue(it, next)
		if !ok {
			return out
		}
		if !e.IsString() {
			cx.IfAbruptCloseIterator(it, func() {
				cx.ThrowTypeError("an element of the list is not a string: " + cx.DisplayString(e))
			})
		}
		// The formatter copies the elements into its parts as they are, lone surrogates included.
		out = append(out, string(cx.ToString(e)))
	}
}

// initListFormat defines Intl.ListFormat.
func (r *realm) initListFormat(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := cx.NewNativeConstructor("ListFormat", 0, r.newListFormat)
	cx.Define(ctor, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	cx.Define(r.ns, cx.Intern("ListFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.listFormatProto = proto

	method(cx, ctor, "supportedLocalesOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(cx, args)
	})
	method(cx, proto, "format", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		lf := thisListFormat(cx, this, "format")
		parts := lf.f.Format(stringListFromIterable(cx, arg(args, 0)))
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
			checkStringLength(cx, n)
			previous = v
		}
		var b wtf8.Builder
		b.Grow(n)
		for _, p := range parts {
			b.WriteString(wtf8.String(p.Value))
		}
		return cx.NewString(b.String())
	})
	method(cx, proto, "formatToParts", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		lf := thisListFormat(cx, this, "formatToParts")
		parts := lf.f.Format(stringListFromIterable(cx, arg(args, 0)))
		return partsArray(cx, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, ""
		})
	})
	method(cx, proto, "resolvedOptions", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		lf := thisListFormat(cx, this, "resolvedOptions")
		return resolvedOptions(cx, "locale", lf.locale, "type", lf.typ, "style", lf.style)
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Intl.ListFormat"), engine.FlagConfigurable)
}

// resolvedOptions returns a new object with the string properties of the pairs of names and
// values kv, in order, leaving out the empty values.
func resolvedOptions(cx *engine.ExecutionContext, kv ...string) engine.Value {
	var keyStorage [32]engine.PropKey
	var valueStorage [32]engine.Value
	keys := keyStorage[:0]
	vals := valueStorage[:0]
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] != "" {
			keys = append(keys, engine.NamedKey(cx.Intern(wtf8.String(kv[i]))))
			vals = append(vals, cx.NewString(wtf8.String(kv[i+1])))
		}
	}
	return engine.ObjectValue(cx.NewObjectFromProperties(keys, vals))
}
