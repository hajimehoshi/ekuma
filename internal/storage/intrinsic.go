// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// Intrinsic identifies an intrinsic object.
type Intrinsic uint8

const (
	IntrinsicObjectPrototype Intrinsic = iota
	IntrinsicFunctionPrototype
	IntrinsicArrayPrototype
	IntrinsicStringPrototype
	IntrinsicNumberPrototype
	IntrinsicBooleanPrototype
	IntrinsicSymbolPrototype
	IntrinsicBigIntPrototype
	IntrinsicIteratorPrototype
	IntrinsicAsyncIteratorPrototype
	IntrinsicArrayIteratorPrototype
	IntrinsicStringIteratorPrototype
	IntrinsicMapIteratorPrototype
	IntrinsicSetIteratorPrototype
	IntrinsicRegExpStringIteratorPrototype
	IntrinsicIteratorHelperPrototype
	IntrinsicWrapForValidIteratorPrototype
	IntrinsicGeneratorPrototype
	IntrinsicAsyncGeneratorPrototype
	IntrinsicGeneratorFunctionPrototype
	IntrinsicAsyncFunctionPrototype
	IntrinsicAsyncGeneratorFunctionPrototype
	IntrinsicRegExpPrototype
	IntrinsicPromisePrototype
	IntrinsicMapPrototype
	IntrinsicSetPrototype
	IntrinsicDatePrototype
	IntrinsicWeakMapPrototype
	IntrinsicWeakSetPrototype
	IntrinsicWeakRefPrototype
	IntrinsicFinalizationRegistryPrototype
	IntrinsicDisposableStackPrototype
	IntrinsicAsyncDisposableStackPrototype
	IntrinsicShadowRealmPrototype
	IntrinsicAbstractModuleSourcePrototype
	IntrinsicPromise
	IntrinsicArray
	IntrinsicObject
	IntrinsicFunction
	IntrinsicRegExp
	IntrinsicIterator
	IntrinsicAbstractModuleSource

	IntrinsicTemporal

	IntrinsicIntl
	IntrinsicIntlDateTimeFormat
	IntrinsicIntlNumberFormat
	IntrinsicIntlSegmentsPrototype
	IntrinsicIntlSegmentIteratorPrototype

	// The Temporal namespace initializes this range of prototypes.
	IntrinsicTemporalInstantPrototype
	IntrinsicTemporalDurationPrototype
	IntrinsicTemporalPlainDatePrototype
	IntrinsicTemporalPlainTimePrototype
	IntrinsicTemporalPlainDateTimePrototype
	IntrinsicTemporalPlainYearMonthPrototype
	IntrinsicTemporalPlainMonthDayPrototype
	IntrinsicTemporalZonedDateTimePrototype

	// The Intl namespace initializes this range of prototypes.
	IntrinsicIntlCollatorPrototype
	IntrinsicIntlDateTimeFormatPrototype
	IntrinsicIntlDisplayNamesPrototype
	IntrinsicIntlDurationFormatPrototype
	IntrinsicIntlListFormatPrototype
	IntrinsicIntlLocalePrototype
	IntrinsicIntlNumberFormatPrototype
	IntrinsicIntlPluralRulesPrototype
	IntrinsicIntlRelativeTimeFormatPrototype
	IntrinsicIntlSegmenterPrototype

	// The prototypes of the native error types are in the order of engine.ErrorKind.
	IntrinsicErrorPrototype
	IntrinsicTypeErrorPrototype
	IntrinsicRangeErrorPrototype
	IntrinsicReferenceErrorPrototype
	IntrinsicSyntaxErrorPrototype
	IntrinsicEvalErrorPrototype
	IntrinsicURIErrorPrototype
	IntrinsicAggregateErrorPrototype
	IntrinsicSuppressedErrorPrototype

	IntrinsicThrowTypeError

	// The getters of the caller and arguments properties of non-strict ordinary functions.
	IntrinsicFunctionCallerGetter
	IntrinsicFunctionArgumentsGetter

	// The binary built-ins are initialized together.
	IntrinsicArrayBuffer
	IntrinsicArrayBufferPrototype
	IntrinsicSharedArrayBuffer
	IntrinsicSharedArrayBufferPrototype
	IntrinsicDataViewPrototype
	IntrinsicTypedArrayPrototypeLengthGetter

	// The typed array constructors are in the order of [element.Type].
	IntrinsicInt8Array
	IntrinsicUint8Array
	IntrinsicUint8ClampedArray
	IntrinsicInt16Array
	IntrinsicUint16Array
	IntrinsicInt32Array
	IntrinsicUint32Array
	IntrinsicFloat16Array
	IntrinsicFloat32Array
	IntrinsicFloat64Array
	IntrinsicBigInt64Array
	IntrinsicBigUint64Array

	// The prototypes of the typed array constructors are in the order of [element.Type].
	IntrinsicInt8ArrayPrototype
	IntrinsicUint8ArrayPrototype
	IntrinsicUint8ClampedArrayPrototype
	IntrinsicInt16ArrayPrototype
	IntrinsicUint16ArrayPrototype
	IntrinsicInt32ArrayPrototype
	IntrinsicUint32ArrayPrototype
	IntrinsicFloat16ArrayPrototype
	IntrinsicFloat32ArrayPrototype
	IntrinsicFloat64ArrayPrototype
	IntrinsicBigInt64ArrayPrototype
	IntrinsicBigUint64ArrayPrototype

	// The initial values of these functions are what the engine treats specially.
	IntrinsicFunctionPrototypeCall
	IntrinsicFunctionPrototypeApply
	IntrinsicEval
	IntrinsicFunctionPrototypeHasInstance
	IntrinsicArrayPrototypeValues
	IntrinsicArrayPrototypePush
	IntrinsicArrayPrototypePop
	IntrinsicArrayIteratorPrototypeNext
	IntrinsicArraySpeciesGetter
	IntrinsicGeneratorPrototypeNext

	// The initial values of these functions are what the built-ins treat specially.
	IntrinsicArrayPrototypeToString
	IntrinsicRegExpPrototypeExec
	IntrinsicMapPrototypeSet
	IntrinsicMapPrototypeEntries
	IntrinsicMapIteratorPrototypeNext
	IntrinsicSetPrototypeAdd
	IntrinsicSetPrototypeHas
	IntrinsicSetPrototypeValues
	IntrinsicSetIteratorPrototypeNext
	IntrinsicWeakMapPrototypeSet
	IntrinsicWeakSetPrototypeAdd

	// The functions that the engine calls.
	IntrinsicJSONParse

	intrinsicCount
)

// IsBinary reports whether i belongs to the binary built-in family.
func (i Intrinsic) IsBinary() bool {
	return IntrinsicArrayBuffer <= i && i <= IntrinsicBigUint64ArrayPrototype
}

// Intrinsic returns the intrinsic object id of r, or nil before it is set.
func (r *RealmPart) Intrinsic(id Intrinsic) *Object {
	return r.Intrinsics[id]
}

// SetIntrinsic makes o the intrinsic object id.
func (r *RealmPart) SetIntrinsic(id Intrinsic, o *Object) {
	r.Intrinsics[id] = o
}
