// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/regexp"
)

// NewRegExpObject returns a new RegExp object with the shape s, whose only property is lastIndex,
// and the internal state state.
func NewRegExpObject(s *Shape, state *RegExpState) *Object {
	o, slots := NewObjectWithStorage(1)
	o.Kind = KindObject
	o.Class = ClassRegExp
	o.Shape = s
	o.InitSlots(slots)
	o.Slots[0] = Number(0)
	o.Ext = state
	return o
}

// RegExpShape returns the shape of a new RegExp object whose prototype is %RegExp.prototype%.
func (r *RealmPart) RegExpShape() *Shape {
	proto := r.Intrinsic(IntrinsicRegExpPrototype)
	if s := r.Shapes.regExp; s != nil && s.Proto == proto {
		return s
	}
	s := r.ShapeFor(proto).With(r.Key(KeyLastIndex), FlagWritable)
	r.Shapes.regExp = s
	return s
}

// hasRegExpShape reports whether o has the shape of a new RegExp object whose prototype is
// %RegExp.prototype%, so that its only own property is a writable lastIndex in slot 0.
func (r *RealmPart) hasRegExpShape(o *Object) bool {
	if ValidateExec {
		r.checkRegExpShape(o)
	}
	s := r.Shapes.regExp
	return s != nil && o.Shape == s
}

// RegExpLastIndex is quiet.Context.RegExpLastIndex.
func (r *RealmPart) RegExpLastIndex(o *Object) (Value, bool) {
	if !r.hasRegExpShape(o) {
		return Undefined, false
	}
	return o.Slots[0], true
}

// SetRegExpLastIndex is quiet.Context.SetRegExpLastIndex.
func (r *RealmPart) SetRegExpLastIndex(o *Object, v Value) bool {
	if !r.hasRegExpShape(o) {
		return false
	}
	o.Slots[0] = v
	r.TouchProto(o)
	return true
}

// RenewRegExp makes the RegExp object o like a new one whose shape is s.
func (o *Object) RenewRegExp(s *Shape) {
	// Only the pointers that differ from those of a new object are stored, so that reusing o needs
	// no write barrier while the garbage collector marks.
	if o.Shape != s {
		o.Shape = s
	}
	SetNumber(&o.Slots[0], 0)
}

// regExpOriginals is what quiet.Context.UnmodifiedRegExpMatcher compares against.
type regExpOriginals struct {
	props []originalProp

	// data holds the data properties of props, found at epoch. Assigning to a data property
	// does not move ProtoEpoch, so their values are compared at every check.
	data []originalData

	// epoch is the value of agent.ProtoEpoch when the slots of props were last found.
	epoch   uint32
	checked bool
	// found reports whether every property of props had its recorded attributes at epoch,
	// and every accessor property its recorded getter.
	found bool
}

// originalProp is a recorded own property of an object.
type originalProp struct {
	holder *Object
	key    *Object
	flags  uint8
	// value is the value of a data property, or the getter of an accessor property.
	value *Object
	// slot is the slot of the property in holder, found at regExpOriginals.epoch.
	slot int32
}

// originalData is a recorded data property of an object.
type originalData struct {
	// ref points at the value of the property, which is in the slots of its holder.
	ref   *Value
	value *Object
}

// find looks the property up in its holder, and reports whether it has the recorded attributes,
// and the recorded getter if it is an accessor property.
func (p *originalProp) find() bool {
	q, ok := p.holder.Shape.Lookup(p.key)
	if !ok || q.Slot < 0 || q.Flags != p.flags {
		return false
	}
	p.slot = q.Slot
	if q.Flags&FlagAccessor != 0 {
		return p.holder.Slots[q.Slot].Accessor().Getter == p.value
	}
	return true
}

// RecordRegExpOriginals records the current own properties protoKeys of %RegExp.prototype%
// and ctorKeys of %RegExp%, which quiet.Context.UnmodifiedRegExpMatcher requires to stay unchanged.
// Every key must name an existing property whose value, if it is a data property, is an object.
func (r *RealmPart) RecordRegExpOriginals(protoKeys, ctorKeys []*Object) {
	proto := r.Intrinsic(IntrinsicRegExpPrototype)
	ctor := r.Intrinsic(IntrinsicRegExp)
	o := &r.regExpOriginals
	// hasRegExpShape compares against this shape, so it must exist from now on.
	r.RegExpShape()
	o.props = o.props[:0]
	record := func(holder *Object, keys []*Object) {
		for _, k := range keys {
			p, ok := holder.Shape.Lookup(k)
			if !ok || p.Slot < 0 {
				panic("engine: RecordRegExpOriginals: missing property " + k.Text)
			}
			v := holder.Slots[p.Slot]
			var val *Object
			if p.Flags&FlagAccessor != 0 {
				val = v.Accessor().Getter
			} else {
				val = v.O
			}
			if val == nil {
				panic("engine: RecordRegExpOriginals: property " + k.Text + " holds no object")
			}
			o.props = append(o.props, originalProp{
				holder: holder,
				key:    k,
				flags:  p.Flags,
				value:  val,
				slot:   p.Slot,
			})
		}
	}
	record(proto, protoKeys)
	record(ctor, ctorKeys)
	// Adding, removing or redefining a property of an object marked as a prototype moves
	// ProtoEpoch. Assigning to a data property does not.
	ctor.Flags |= ObjIsProto
	o.checked = false
}

// UnmodifiedRegExpMatcher is quiet.Context.UnmodifiedRegExpMatcher.
func (r *RealmPart) UnmodifiedRegExpMatcher(o *Object) *regexp.Regexp {
	if o.Class != ClassRegExp || !r.hasRegExpShape(o) {
		return nil
	}
	matcher := o.ExtState().(*RegExpState).Matcher
	if matcher == nil {
		return nil
	}
	rec := &r.regExpOriginals
	if !rec.checked || rec.epoch != r.ProtoEpoch {
		rec.found = true
		rec.data = rec.data[:0]
		for i := range rec.props {
			p := &rec.props[i]
			if !p.find() {
				rec.found = false
				break
			}
			if p.flags&FlagAccessor == 0 {
				rec.data = append(rec.data, originalData{
					ref:   &p.holder.Slots[p.slot],
					value: p.value,
				})
			}
		}
		rec.epoch = r.ProtoEpoch
		rec.checked = true
	}
	if !rec.found {
		return nil
	}
	for _, d := range rec.data {
		if d.ref.O != d.value {
			return nil
		}
	}
	if ValidateExec {
		r.checkUnmodifiedRegExp(o)
	}
	return matcher
}

// matchResultShapes are the shapes that a match result array has after
// ExecutionContext.DefineMatchResultProperties or ExecutionContext.DefineMatchResultPropertiesWithIndices.
type matchResultShapes struct {
	// array is the shape of a new array that the others derive from.
	array *Shape

	// plain has index, input and groups, and withIndices also indices.
	plain, withIndices *Shape
}

// matchResultShapesFor returns the match result shapes that derive from the shape array of a
// new array of the current realm.
func (r *RealmPart) matchResultShapesFor(array *Shape) *matchResultShapes {
	m := &r.matchResultShapes
	if m.array != array {
		plain := array.With(r.Key(KeyIndex), FlagsData).With(r.Key(KeyInput), FlagsData).With(r.Key(KeyGroups), FlagsData)
		*m = matchResultShapes{
			array:       array,
			plain:       plain,
			withIndices: plain.With(r.Intern("indices"), FlagsData),
		}
	}
	return m
}

// DefineMatchResultProperties creates the data properties index, input and groups of a match
// result on a, in that order, as CreateDataPropertyOrThrow does. a must be an array from
// ExecutionContext.NewArrayWithRoom that has no named properties.
func (r *RealmPart) DefineMatchResultProperties(a *Object, index, input, groups Value) {
	if a.Shape != r.Shapes.Array || len(a.Slots) != 0 {
		panic("engine: DefineMatchResultProperties on an array that is not new")
	}
	a.Shape = r.matchResultShapesFor(a.Shape).plain
	a.Slots = append(a.Slots, index, input, groups)
}

// DefineMatchResultPropertiesWithIndices is like ExecutionContext.DefineMatchResultProperties and creates
// the data property indices after groups.
func (r *RealmPart) DefineMatchResultPropertiesWithIndices(a *Object, index, input, groups, indices Value) {
	if a.Shape != r.Shapes.Array || len(a.Slots) != 0 {
		panic("engine: DefineMatchResultPropertiesWithIndices on an array that is not new")
	}
	a.Shape = r.matchResultShapesFor(a.Shape).withIndices
	a.Slots = append(a.Slots, index, input, groups, indices)
}
