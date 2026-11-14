// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/regexp"
)

// regExpState holds the internal slots of a RegExp object. It does not change once it is
// set, so objects can share it.
type regExpState struct {
	matcher *regexp.Regexp
	source  string
	flags   string
	legacy  bool
}

// regExpSite is a regular expression literal of a function.
type regExpSite struct {
	literal bytecode.RegExp

	// state is the internal state that the RegExp objects of the literal share, once the
	// pattern is compiled.
	state *regExpState
}

// regExpCacheLimit bounds the number of compiled patterns that a [VM] keeps.
const regExpCacheLimit = 1024

// RegExpAlloc implements RegExpAlloc: it returns a RegExp object for the constructor
// newTarget, with an own lastIndex property and no matcher yet.
func (vm *VM) RegExpAlloc(newTarget *Object) *Object {
	legacy := newTarget == vm.Intrinsic(IntrinsicRegExp)
	var s *shape
	if legacy {
		// The prototype property of %RegExp% is neither writable nor configurable.
		s = vm.regExpShape()
	} else {
		proto := vm.GetPrototypeFromConstructor(newTarget, vm.Intrinsic(IntrinsicRegExpPrototype))
		s = vm.shapeFor(proto).with(vm.Key(keyLastIndex), FlagWritable)
	}
	return vm.newRegExpObject(s, &regExpState{
		legacy: legacy,
	})
}

// regExpShape returns the shape of a new RegExp object whose prototype is %RegExp.prototype%.
func (vm *VM) regExpShape() *shape {
	proto := vm.Intrinsic(IntrinsicRegExpPrototype)
	if s := vm.shapes.regExp; s != nil && s.proto == proto {
		return s
	}
	s := vm.shapeFor(proto).with(vm.Key(keyLastIndex), FlagWritable)
	vm.shapes.regExp = s
	return s
}

// newRegExpObject returns a new RegExp object with the shape s, whose only property is
// lastIndex, and the internal state st.
func (vm *VM) newRegExpObject(s *shape, st *regExpState) *Object {
	return &Object{
		kind:  kindObject,
		class: classRegExp,
		shape: s,
		slots: []Value{{n: 0}},
		ext:   st,
	}
}

// RegExpInitialize implements RegExpInitialize for the RegExp object o and returns o.
// It throws a SyntaxError for an invalid pattern or invalid flags.
func (vm *VM) RegExpInitialize(o *Object, pattern, flags Value) *Object {
	p := ""
	if !pattern.IsUndefined() {
		p = vm.ToString(pattern)
	}
	f := ""
	if !flags.IsUndefined() {
		f = vm.ToString(flags)
	}
	re := vm.compileRegExp(p, f)
	o.ext = &regExpState{
		matcher: re,
		source:  p,
		flags:   f,
		legacy:  o.ext.(*regExpState).legacy,
	}
	vm.Set(o, NamedKey(vm.Key(keyLastIndex)), Value{n: 0})
	return o
}

// compileRegExp compiles the pattern p with the flags f, or throws a SyntaxError.
func (vm *VM) compileRegExp(p, f string) *regexp.Regexp {
	key := f + "/" + p
	if re, ok := vm.regExps[key]; ok {
		return re
	}
	flags, err := regexp.ParseFlags(f)
	if err != nil {
		vm.throwSyntaxError(err.Error())
	}
	re, err := regexp.Compile(p, flags)
	if err != nil {
		vm.throwSyntaxError(err.Error())
	}
	if vm.regExps == nil || len(vm.regExps) >= regExpCacheLimit {
		vm.regExps = map[string]*regexp.Regexp{}
	}
	vm.regExps[key] = re
	return re
}

// newRegExpLiteral evaluates the regular expression literal site: it returns a new RegExp object.
func (vm *VM) newRegExpLiteral(site *regExpSite) *Object {
	if site.state == nil {
		site.state = &regExpState{
			matcher: vm.compileRegExp(site.literal.Pattern, site.literal.Flags),
			source:  site.literal.Pattern,
			flags:   site.literal.Flags,
			legacy:  true,
		}
	}
	return vm.newRegExpObject(vm.regExpShape(), site.state)
}

// RegExpMatcher returns the compiled pattern of the RegExp object o,
// or nil when o is not a RegExp object.
func (o *Object) RegExpMatcher() *regexp.Regexp {
	if o.class != classRegExp {
		return nil
	}
	return o.ext.(*regExpState).matcher
}

// RegExpOriginal returns the original source and flags of the RegExp object o.
func (o *Object) RegExpOriginal() (source, flags string) {
	st := o.ext.(*regExpState)
	return st.source, st.flags
}

// RegExpLegacyFeaturesEnabled implements the [[LegacyFeaturesEnabled]] slot of the RegExp
// object o: whether o was created for the RegExp constructor itself.
func (o *Object) RegExpLegacyFeaturesEnabled() bool {
	return o.ext.(*regExpState).legacy
}
