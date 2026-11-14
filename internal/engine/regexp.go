// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/regexp"
)

// regExpState holds the internal slots of a RegExp object.
type regExpState struct {
	matcher *regexp.Regexp
	source  string
	flags   string
	legacy  bool
}

// regExpCacheLimit bounds the number of compiled patterns that a VM keeps.
const regExpCacheLimit = 1024

// RegExpAlloc implements RegExpAlloc: it returns a RegExp object for the constructor
// newTarget, with an own lastIndex property and no matcher yet.
func (vm *VM) RegExpAlloc(newTarget *Object) *Object {
	o := vm.NewObject(vm.GetPrototypeFromConstructor(newTarget, vm.Intrinsic(IntrinsicRegExpPrototype)))
	o.class = classRegExp
	o.ext = &regExpState{
		legacy: newTarget == vm.Intrinsic(IntrinsicRegExp),
	}
	vm.Define(o, vm.Key(keyLastIndex), Value{n: 0}, FlagWritable)
	return o
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
	st := o.ext.(*regExpState)
	st.matcher = re
	st.source = p
	st.flags = f
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

// newRegExpLiteral returns the RegExp object of a regular expression literal.
func (vm *VM) newRegExpLiteral(pattern, flags Value) Value {
	o := vm.RegExpAlloc(vm.Intrinsic(IntrinsicRegExp))
	return Value{o: vm.RegExpInitialize(o, pattern, flags)}
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
