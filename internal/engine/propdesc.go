// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import "math/bits"

// Fields of a property descriptor that are present.
const (
	HasValue uint8 = 1 << iota
	HasWritable
	HasGet
	HasSet
	HasEnumerable
	HasConfigurable
)

// PropDesc is a property descriptor.
type PropDesc struct {
	value  Value
	getter *Object
	setter *Object

	// flags holds the attributes. Only the ones that has names are meaningful.
	flags uint8
	has   uint8
}

// Value returns the [[Value]] field of the data descriptor d.
func (d *PropDesc) Value() Value {
	return d.value
}

// Getter returns the [[Get]] field of the accessor descriptor d, or nil when it is undefined.
func (d *PropDesc) Getter() *Object {
	return d.getter
}

// Setter returns the [[Set]] field of the accessor descriptor d, or nil when it is undefined.
func (d *PropDesc) Setter() *Object {
	return d.setter
}

// Writable reports the [[Writable]] field of d, which is false when absent.
func (d *PropDesc) Writable() bool {
	return d.flags&FlagWritable != 0
}

// Enumerable reports the [[Enumerable]] field of d, which is false when absent.
func (d *PropDesc) Enumerable() bool {
	return d.flags&FlagEnumerable != 0
}

// Configurable reports the [[Configurable]] field of d, which is false when absent.
func (d *PropDesc) Configurable() bool {
	return d.flags&FlagConfigurable != 0
}

// SetValue sets the [[Value]] field of d to v.
func (d *PropDesc) SetValue(v Value) {
	d.value = v
	d.has |= HasValue
}

// SetGetter sets the [[Get]] field of d to f, where nil stands for undefined.
func (d *PropDesc) SetGetter(f *Object) {
	d.getter = f
	d.has |= HasGet
}

// SetSetter sets the [[Set]] field of d to f, where nil stands for undefined.
func (d *PropDesc) SetSetter(f *Object) {
	d.setter = f
	d.has |= HasSet
}

// SetWritable sets the [[Writable]] field of d to b.
func (d *PropDesc) SetWritable(b bool) {
	d.setFlag(HasWritable, FlagWritable, b)
}

// SetEnumerable sets the [[Enumerable]] field of d to b.
func (d *PropDesc) SetEnumerable(b bool) {
	d.setFlag(HasEnumerable, FlagEnumerable, b)
}

// SetConfigurable sets the [[Configurable]] field of d to b.
func (d *PropDesc) SetConfigurable(b bool) {
	d.setFlag(HasConfigurable, FlagConfigurable, b)
}

// setFlag makes the field has of d present with the attribute flag set to b.
func (d *PropDesc) setFlag(has, flag uint8, b bool) {
	d.has |= has
	if b {
		d.flags |= flag
	} else {
		d.flags &^= flag
	}
}

// IsAccessor implements IsAccessorDescriptor.
func (d *PropDesc) IsAccessor() bool {
	return d.has&(HasGet|HasSet) != 0
}

// IsData implements IsDataDescriptor.
func (d *PropDesc) IsData() bool {
	return d.has&(HasValue|HasWritable) != 0
}

func dataDesc(v Value, flags uint8) PropDesc {
	return PropDesc{
		value: v,
		flags: flags,
		has:   HasValue | HasWritable | HasEnumerable | HasConfigurable,
	}
}

// valueDesc returns a descriptor with the [[Value]] field v and no others.
func valueDesc(v Value) PropDesc {
	return PropDesc{
		value: v,
		has:   HasValue,
	}
}

// accessorDesc returns the descriptor of an accessor property with the content a of its slot and
// the attributes flags, which include [FlagAccessor].
func accessorDesc(a *accessorPair, flags uint8) PropDesc {
	return PropDesc{
		getter: a.getter,
		setter: a.setter,
		flags:  flags,
		has:    HasGet | HasSet | HasEnumerable | HasConfigurable,
	}
}

// hasField reports whether the field has of d is present.
func (d *PropDesc) hasField(has uint8) bool {
	return d.has&has != 0
}

// hasTrue reports whether the field has of d, for the attribute flag, is present and true.
func (d *PropDesc) hasTrue(has, flag uint8) bool {
	return d.has&has != 0 && d.flags&flag != 0
}

// hasFalse reports whether the field has of d, for the attribute flag, is present and false.
func (d *PropDesc) hasFalse(has, flag uint8) bool {
	return d.has&has != 0 && d.flags&flag == 0
}

// fieldCount returns the number of fields of d that are present.
func (d *PropDesc) fieldCount() int {
	return bits.OnesCount8(d.has)
}

// newData returns the value and the attributes of the new data property that d, which is no
// accessor descriptor, defines.
func (d *PropDesc) newData() (Value, uint8) {
	val := d.value
	if d.has&HasValue == 0 {
		val = Undefined
	}
	return val, d.flags & d.attrMask()
}

// newAccessorProperty returns the content of the slot and the attributes of the new accessor
// property that d, which is an accessor descriptor, defines.
func (d *PropDesc) newAccessorProperty() (Value, uint8) {
	return newAccessor(d.getter, d.setter), d.flags&d.attrMask()&^FlagWritable | FlagAccessor
}

// keepsData reports whether d redefines the existing data property described by cur as a data property.
func (d *PropDesc) keepsData(cur *PropDesc) bool {
	return d.has&(HasGet|HasSet) == 0 && cur.flags&FlagAccessor == 0
}

// redefinedData returns the value and the attributes that the existing data property described by
// cur has once d, for which keepsData holds, redefined it.
func (d *PropDesc) redefinedData(cur *PropDesc) (Value, uint8) {
	val := cur.value
	if d.has&HasValue != 0 {
		val = d.value
	}
	m := d.attrMask()
	return val, cur.flags&^m | d.flags&m
}

// redefinedProperty returns the content of the slot and the attributes that the existing property
// described by cur has once d, for which keepsData does not hold, redefined it.
func (d *PropDesc) redefinedProperty(cur *PropDesc) (Value, uint8) {
	m := d.attrMask()
	flags := cur.flags&^m | d.flags&m
	switch {
	case d.IsAccessor():
		g, s := cur.getter, cur.setter
		if cur.flags&FlagAccessor == 0 {
			g, s = nil, nil
		}
		if d.has&HasGet != 0 {
			g = d.getter
		}
		if d.has&HasSet != 0 {
			s = d.setter
		}
		return newAccessor(g, s), flags&^FlagWritable | FlagAccessor
	case !d.IsData():
		return newAccessor(cur.getter, cur.setter), flags
	}
	// A data property takes the place of an accessor property.
	val := Undefined
	if d.has&HasValue != 0 {
		val = d.value
	}
	return val, cur.flags&^(FlagAccessor|FlagWritable|m) | d.flags&m
}

// attrMask returns the attribute bits that d specifies.
func (d *PropDesc) attrMask() uint8 {
	var m uint8
	if d.has&HasEnumerable != 0 {
		m |= FlagEnumerable
	}
	if d.has&HasConfigurable != 0 {
		m |= FlagConfigurable
	}
	if d.has&HasWritable != 0 {
		m |= FlagWritable
	}
	return m
}

// isCompatiblePropertyDescriptor implements IsCompatiblePropertyDescriptor. hasCur reports whether
// the property described by cur exists.
func isCompatiblePropertyDescriptor(extensible bool, d, cur *PropDesc, hasCur bool) bool {
	if !hasCur {
		return extensible
	}
	return cur.flags&FlagConfigurable != 0 || nonConfigurableCompatible(cur, d)
}

// nonConfigurableCompatible reports whether ValidateAndApplyPropertyDescriptor accepts d for the
// existing property described by cur, which is not configurable.
func nonConfigurableCompatible(cur, d *PropDesc) bool {
	if d.has&HasConfigurable != 0 && d.flags&FlagConfigurable != 0 {
		return false
	}
	if d.has&HasEnumerable != 0 && (d.flags^cur.flags)&FlagEnumerable != 0 {
		return false
	}
	curAccessor := cur.flags&FlagAccessor != 0
	if d.IsAccessor() != curAccessor && (d.IsAccessor() || d.IsData()) {
		return false
	}
	if curAccessor {
		if d.has&HasGet != 0 && d.getter != cur.getter {
			return false
		}
		if d.has&HasSet != 0 && d.setter != cur.setter {
			return false
		}
	} else if cur.flags&FlagWritable == 0 {
		if d.has&HasWritable != 0 && d.flags&FlagWritable != 0 {
			return false
		}
		if d.has&HasValue != 0 && !SameValue(d.value, cur.value) {
			return false
		}
	}
	return true
}

// complete implements CompletePropertyDescriptor. It also marks an accessor descriptor with
// [FlagAccessor], as the descriptors of existing properties are.
func (d *PropDesc) complete() {
	if d.IsAccessor() {
		d.has |= HasGet | HasSet
		d.flags = d.flags&^FlagWritable | FlagAccessor
	} else {
		if d.has&HasValue == 0 {
			d.value = Undefined
		}
		if d.has&HasWritable == 0 {
			d.flags &^= FlagWritable
		}
		d.has |= HasValue | HasWritable
	}
	if d.has&HasEnumerable == 0 {
		d.flags &^= FlagEnumerable
	}
	if d.has&HasConfigurable == 0 {
		d.flags &^= FlagConfigurable
	}
	d.has |= HasEnumerable | HasConfigurable
}

// newAccessor returns the content of the slot of an accessor property with the given getter
// and setter.
func newAccessor(getter, setter *Object) Value {
	return Value{
		o: &Object{
			kind: kindAccessor,
			ext: &accessorPair{
				getter: getter,
				setter: setter,
			},
		},
	}
}

type accessorPair struct {
	getter *Object
	setter *Object
}

func (v Value) accessor() *accessorPair {
	return v.o.ext.(*accessorPair)
}

// addGetter makes f the getter of a, and reports false when a has a getter.
func (a *accessorPair) addGetter(f *Object) bool {
	if a.getter != nil {
		return false
	}
	a.getter = f
	return true
}

// addSetter makes f the setter of a, and reports false when a has a setter.
func (a *accessorPair) addSetter(f *Object) bool {
	if a.setter != nil {
		return false
	}
	a.setter = f
	return true
}
