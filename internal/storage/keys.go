// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// CommonKey identifies a string property key that the engine provides.
type CommonKey uint8

const (
	KeyLength CommonKey = iota
	KeyPrototype
	KeyConstructor
	KeyName
	KeyMessage
	KeyStack
	KeyCause
	KeyErrors
	KeyError
	KeySuppressed
	KeyToString
	KeyValueOf
	KeyToJSON
	KeyCall
	KeyApply
	KeyBind
	KeyGet
	KeySet
	KeyValue
	KeyWritable
	KeyEnumerable
	KeyConfigurable
	KeyNext
	KeyDone
	KeyReturn
	KeyThrow
	KeyThen
	KeyCallee
	KeyCaller
	KeyArguments
	KeyIndex
	KeyInput
	KeyGroups
	KeyLastIndex
	KeyRaw
	KeySource
	KeyFlags
	KeyDefault
	KeyGetPrototypeOf
	KeySetPrototypeOf
	KeyIsExtensible
	KeyPreventExtensions
	KeyGetOwnPropertyDescriptor
	KeyDefineProperty
	KeyHas
	KeyDeleteProperty
	KeyOwnKeys
	KeyConstruct
	KeyAdd
	KeyKeys
	KeySize
	KeyStatus
	KeyReason
	KeyMode
	KeyPadding
	KeyToLocaleString
	KeyToISOString
	KeyHasIndices
	KeyGlobal
	KeyIgnoreCase
	KeyMultiline
	KeyDotAll
	KeyUnicode
	KeyUnicodeSets
	KeySticky
	KeyExec
	CommonKeyCount
)

// WellKnownSymbol identifies a well-known symbol.
type WellKnownSymbol uint8

const (
	SymbolIterator WellKnownSymbol = iota
	SymbolAsyncIterator
	SymbolHasInstance
	SymbolToPrimitive
	SymbolToStringTag
	SymbolSpecies
	SymbolIsConcatSpreadable
	SymbolUnscopables
	SymbolMatch
	SymbolMatchAll
	SymbolReplace
	SymbolSearch
	SymbolSplit
	SymbolDispose
	SymbolAsyncDispose
	SymbolMetadata
	WellKnownSymbolCount
)

// keys are the property keys that the engine itself uses.
type keys struct {
	Common    [CommonKeyCount]*Object
	WellKnown [WellKnownSymbolCount]*Object
}

// Key returns the string property key k.
func (sp *Space) Key(k CommonKey) *Object {
	return sp.Keys.Common[k]
}

// WellKnownSymbol returns the well-known symbol s.
func (sp *Space) WellKnownSymbol(s WellKnownSymbol) *Object {
	return sp.Keys.WellKnown[s]
}
