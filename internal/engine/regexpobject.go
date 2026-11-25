// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import "github.com/hajimehoshi/ekuma/internal/regexp"

// This file holds the storage of RegExp objects and of match results.

// newRegExpObject returns a new RegExp object with the shape s, whose only property is lastIndex,
// and the internal state state.
func newRegExpObject(s *shape, state any) *Object {
	o, slots := newObjectWithStorage(1)
	o.kind = kindObject
	o.class = classRegExp
	o.shape = s
	o.initSlots(slots)
	o.slots[0] = Number(0)
	o.ext = state
	return o
}

// regExpShape returns the shape of a new RegExp object whose prototype is %RegExp.prototype%.
func (r *realmPart) regExpShape() *shape {
	proto := r.Intrinsic(IntrinsicRegExpPrototype)
	if s := r.shapes.regExp; s != nil && s.proto == proto {
		return s
	}
	s := r.shapeFor(proto).with(r.Key(KeyLastIndex), FlagWritable)
	r.shapes.regExp = s
	return s
}

// hasRegExpShape reports whether o has the shape of a new RegExp object whose prototype is
// %RegExp.prototype%, so that its only own property is a writable lastIndex in slot 0.
func (r *realmPart) hasRegExpShape(o *Object) bool {
	if validateExec {
		r.checkRegExpShape(o)
	}
	s := r.shapes.regExp
	return s != nil && o.shape == s
}

// regExpLastIndex is [QuietContext.RegExpLastIndex].
func (r *realmPart) regExpLastIndex(o *Object) (Value, bool) {
	if !r.hasRegExpShape(o) {
		return Undefined, false
	}
	return o.slots[0], true
}

// setRegExpLastIndex is [QuietContext.SetRegExpLastIndex].
func (r *realmPart) setRegExpLastIndex(o *Object, v Value) bool {
	if !r.hasRegExpShape(o) {
		return false
	}
	o.slots[0] = v
	r.touchProto(o)
	return true
}

// renewRegExp makes the RegExp object o like a new one whose shape is s.
func (o *Object) renewRegExp(s *shape) {
	// Only the pointers that differ from those of a new object are stored, so that reusing o needs
	// no write barrier while the garbage collector marks.
	if o.shape != s {
		o.shape = s
	}
	setNumber(&o.slots[0], 0)
}

// regExpOriginals is what [QuietContext.UnmodifiedRegExpMatcher] compares against.
type regExpOriginals struct {
	props []originalProp

	// data holds the data properties of props, found at epoch. Assigning to a data property
	// does not move protoEpoch, so their values are compared at every check.
	data []originalData

	// epoch is the value of agent.protoEpoch when the slots of props were last found.
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
	q, ok := p.holder.shape.lookup(p.key)
	if !ok || q.slot < 0 || q.flags != p.flags {
		return false
	}
	p.slot = q.slot
	if q.flags&FlagAccessor != 0 {
		return p.holder.slots[q.slot].accessor().getter == p.value
	}
	return true
}

// RecordRegExpOriginals records the current own properties protoKeys of %RegExp.prototype%
// and ctorKeys of %RegExp%, which [QuietContext.UnmodifiedRegExpMatcher] requires to stay unchanged.
// Every key must name an existing property whose value, if it is a data property, is an object.
func (r *realmPart) RecordRegExpOriginals(protoKeys, ctorKeys []*Object) {
	proto := r.Intrinsic(IntrinsicRegExpPrototype)
	ctor := r.Intrinsic(IntrinsicRegExp)
	o := &r.regExpOriginals
	// hasRegExpShape compares against this shape, so it must exist from now on.
	r.regExpShape()
	o.props = o.props[:0]
	record := func(holder *Object, keys []*Object) {
		for _, k := range keys {
			p, ok := holder.shape.lookup(k)
			if !ok || p.slot < 0 {
				panic("engine: RecordRegExpOriginals: missing property " + k.str)
			}
			v := holder.slots[p.slot]
			var val *Object
			if p.flags&FlagAccessor != 0 {
				val = v.accessor().getter
			} else {
				val = v.o
			}
			if val == nil {
				panic("engine: RecordRegExpOriginals: property " + k.str + " holds no object")
			}
			o.props = append(o.props, originalProp{
				holder: holder,
				key:    k,
				flags:  p.flags,
				value:  val,
				slot:   p.slot,
			})
		}
	}
	record(proto, protoKeys)
	record(ctor, ctorKeys)
	// Adding, removing or redefining a property of an object marked as a prototype moves
	// protoEpoch. Assigning to a data property does not.
	ctor.flags |= objIsProto
	o.checked = false
}

// unmodifiedRegExpMatcher is [QuietContext.UnmodifiedRegExpMatcher].
func (r *realmPart) unmodifiedRegExpMatcher(o *Object) *regexp.Regexp {
	if o.class != classRegExp || !r.hasRegExpShape(o) {
		return nil
	}
	matcher := o.extState().(*regExpState).matcher
	if matcher == nil {
		return nil
	}
	rec := &r.regExpOriginals
	if !rec.checked || rec.epoch != r.protoEpoch {
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
					ref:   &p.holder.slots[p.slot],
					value: p.value,
				})
			}
		}
		rec.epoch = r.protoEpoch
		rec.checked = true
	}
	if !rec.found {
		return nil
	}
	for _, d := range rec.data {
		if d.ref.o != d.value {
			return nil
		}
	}
	if validateExec {
		r.checkUnmodifiedRegExp(o)
	}
	return matcher
}

// matchResultShapes are the shapes that a match result array has after
// [ExecutionContext.DefineMatchResultProperties] or [ExecutionContext.DefineMatchResultPropertiesWithIndices].
type matchResultShapes struct {
	// array is the shape of a new array that the others derive from.
	array *shape

	// plain has index, input and groups, and withIndices also indices.
	plain, withIndices *shape
}

// matchResultShapesFor returns the match result shapes that derive from the shape array of a
// new array of the current realm.
func (r *realmPart) matchResultShapesFor(array *shape) *matchResultShapes {
	m := &r.matchResultShapes
	if m.array != array {
		plain := array.with(r.Key(KeyIndex), FlagsData).with(r.Key(keyInput), FlagsData).with(r.Key(KeyGroups), FlagsData)
		*m = matchResultShapes{
			array:       array,
			plain:       plain,
			withIndices: plain.with(r.Intern("indices"), FlagsData),
		}
	}
	return m
}

// DefineMatchResultProperties creates the data properties index, input and groups of a match
// result on a, in that order, as CreateDataPropertyOrThrow does. a must be an array from
// [ExecutionContext.NewArrayWithRoom] that has no named properties.
func (r *realmPart) DefineMatchResultProperties(a *Object, index, input, groups Value) {
	if a.shape != r.shapes.array || len(a.slots) != 0 {
		panic("engine: DefineMatchResultProperties on an array that is not new")
	}
	a.shape = r.matchResultShapesFor(a.shape).plain
	a.slots = append(a.slots, index, input, groups)
}

// DefineMatchResultPropertiesWithIndices is like [ExecutionContext.DefineMatchResultProperties] and creates
// the data property indices after groups.
func (r *realmPart) DefineMatchResultPropertiesWithIndices(a *Object, index, input, groups, indices Value) {
	if a.shape != r.shapes.array || len(a.slots) != 0 {
		panic("engine: DefineMatchResultPropertiesWithIndices on an array that is not new")
	}
	a.shape = r.matchResultShapesFor(a.shape).withIndices
	a.slots = append(a.slots, index, input, groups, indices)
}
