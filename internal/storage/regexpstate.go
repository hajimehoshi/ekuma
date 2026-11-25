// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// RegExpState holds the internal slots of a RegExp object. It does not change once it is
// set, so objects can share it.
type RegExpState struct {
	Matcher *regexp.Regexp
	Source  wtf8.String
	Flags   wtf8.String
	Legacy  bool
	// Lent marks the state of a RegExp object that ExecutionContext.LendRegExpLiterals lends.
	Lent bool

	// Realm is the [[Realm]] of the object.
	Realm *RealmPart
}

// RegExpSite is a regular expression literal of a function.
type RegExpSite struct {
	Literal bytecode.RegExp

	// State is the internal State that the RegExp objects of the literal share, once the
	// pattern is compiled.
	State *RegExpState

	// Lent is the RegExp object that ExecutionContext.LendRegExpLiterals lends for the literal, once made.
	Lent *Object
}

// IsRegExpObject reports whether o is a RegExp object.
func (o *Object) IsRegExpObject() bool {
	return o.Class == ClassRegExp
}

// RegExpMatcher returns the compiled pattern of the RegExp object o,
// or nil when o is not a RegExp object.
func (o *Object) RegExpMatcher() *regexp.Regexp {
	if o.Class != ClassRegExp {
		return nil
	}
	return o.ExtState().(*RegExpState).Matcher
}

// RegExpOriginal returns the original source and flags of the RegExp object o.
func (o *Object) RegExpOriginal() (source, flags wtf8.String) {
	st := o.ExtState().(*RegExpState)
	return st.Source, st.Flags
}

// RegExpLegacyFeaturesEnabled implements the [[LegacyFeaturesEnabled]] slot of the RegExp
// object o: whether o was created for the RegExp constructor itself.
func (o *Object) RegExpLegacyFeaturesEnabled() bool {
	return o.ExtState().(*RegExpState).Legacy
}

// legacyRegExpStatics holds the internal slots of %RegExp% for its legacy static properties.
type legacyRegExpStatics struct {
	// Input and str are undefined when their respective properties are unavailable.
	Input Value
	// Str and caps are the subject and the capture indices of the last match, if any.
	Str  Value
	Caps []int
}
