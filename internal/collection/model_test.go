// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collection_test

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

const (
	// maxModelBytes is the length of the operations that a run of the model reads.
	maxModelBytes = 2 << 10

	// maxModelKeys is the number of live entries up to which a run inserts.
	maxModelKeys = 1 << 10

	// maxCollidingKeys is maxModelKeys for keys with one hash, which cost quadratic time.
	maxCollidingKeys = 1 << 8

	// maxModelSteps is the number of entries that the list of a model has room for, which
	// bounds the time of a run.
	maxModelSteps = 1 << 14

	modelTables  = 3
	modelCursors = 4
)

// modelKeyKinds is the number of kinds of keys, which is the number of cases of newKey.
const modelKeyKinds = 7

// modelSeeds are the seeds of the hashes of the keys, from keys that rarely collide to numbers and
// BigInts that all do. They do not reach the BigInts beyond int64, whose hashes differ from process
// to process.
var modelSeeds = [...][2]uint64{
	{0x9e3779b97f4a7c15, 0xbf58476d1ce4e5b9},
	{0, 1 << 32},
	{0, 0},
}

// keySource makes the keys of a run.
type keySource struct {
	cx      *engine.ExecutionContext
	objects map[int]engine.Value

	// made counts the keys, which varies the representation of equal keys.
	made int
}

// newKey returns the key number id of a kind, in one of its representations.
func (s *keySource) newKey(kind, id int) engine.Value {
	s.made++
	alt := s.made&1 != 0
	switch kind {
	case 0:
		switch id {
		case 0:
			if alt {
				return engine.Number(math.Copysign(0, -1))
			}
			return engine.Number(0)
		case 1:
			if alt {
				return engine.Number(math.Float64frombits(0xfff8_0000_dead_beef))
			}
			return engine.Number(math.NaN())
		}
		return engine.Number(float64(id))
	case 1:
		return engine.Number(float64(id) + 0.5)
	case 2, 3:
		// Strings on both sides of the length up to which the slots locate them. The
		// strings of NUL bytes differ in their lengths only.
		str := strconv.Itoa(id)
		if id < 24 {
			str = strings.Repeat("\x00", id)
		}
		if kind == 3 {
			str += "a string that is not short"
		}
		if alt && len(str) > 1 {
			return s.cx.ConcatStrings([]engine.Value{
				s.cx.NewString(wtf8.FromValidUTF8(str[:1])),
				s.cx.NewString(wtf8.FromValidUTF8(str[1:])),
			})
		}
		return s.cx.NewString(wtf8.FromValidUTF8(str))
	case 4:
		return engine.BigIntFromInt64(math.MinInt64 + int64(id))
	case 5:
		// The smallest magnitudes that an int64 does not hold, of both signs.
		b := new(big.Int).Lsh(big.NewInt(1), 63)
		b.Add(b, big.NewInt(int64(id/2)))
		if id&1 != 0 {
			b.Neg(b).Sub(b, big.NewInt(1))
		}
		return engine.NewBigInt(b)
	default:
		switch id {
		case 0:
			return engine.Undefined
		case 1:
			return engine.Null
		case 2:
			return engine.True
		}
		v, ok := s.objects[id]
		if !ok {
			v = engine.ObjectValue(s.cx.NewObject(nil))
			s.objects[id] = v
		}
		return v
	}
}

// modelKey is the identity of a key. The keys of different identities differ for SameValueZero.
type modelKey struct {
	kind int
	id   int
}

func (k modelKey) String() string {
	return fmt.Sprintf("key %d of kind %d", k.id, k.kind)
}

// modelEntry is an entry of the list of a Map in the specification, which never removes one.
type modelEntry struct {
	key   modelKey
	value float64
	dead  bool
}

// model is the reference for a table.
type model struct {
	list []modelEntry

	// indexOfKey holds the index in list of the live entry of each key.
	indexOfKey map[modelKey]int
}

func (m *model) set(k modelKey, value float64) {
	if i, ok := m.indexOfKey[k]; ok {
		m.list[i].value = value
		return
	}
	m.indexOfKey[k] = len(m.list)
	m.list = append(m.list, modelEntry{
		key:   k,
		value: value,
	})
}

func (m *model) delete(k modelKey) bool {
	i, ok := m.indexOfKey[k]
	if ok {
		m.list[i].dead = true
		delete(m.indexOfKey, k)
	}
	return ok
}

func (m *model) clear() {
	for i := range m.list {
		m.list[i].dead = true
	}
	clear(m.indexOfKey)
}

// skip returns the index of the first live entry from the index i on, or the length of the list.
func (m *model) skip(i int) int {
	for i < len(m.list) && m.list[i].dead {
		i++
	}
	return i
}

// modelCursor is a cursor and its position in the list of the model.
type modelCursor struct {
	cursor collection.Cursor
	pos    int
}

// subject is a table with its model and its open cursors.
type subject struct {
	table   *collection.Table
	model   model
	cursors []modelCursor
}

func newSubject(table *collection.Table) *subject {
	return &subject{
		table: table,
		model: model{
			indexOfKey: map[modelKey]int{},
		},
	}
}

// copyFrom appends the live entries of src to m, which must have none.
func (m *model) copyFrom(src *model) {
	for _, e := range src.list {
		if !e.dead {
			m.set(e.key, e.value)
		}
	}
}

// modelRun executes operations on tables and their models.
type modelRun struct {
	t        *testing.T
	ops      []byte
	keys     keySource
	mixed    bool
	kind     int
	maxKeys  int
	subjects [modelTables]*subject

	// fresh is the number of the next key of a run of insertions.
	fresh int

	// value is the value of the last insertion.
	value int

	failed bool
}

// next returns the next byte of the operations, or 0 at their end.
func (r *modelRun) next() int {
	if len(r.ops) == 0 {
		return 0
	}
	b := r.ops[0]
	r.ops = r.ops[1:]
	return int(b)
}

func (r *modelRun) errorf(format string, args ...any) {
	r.t.Helper()
	r.failed = true
	r.t.Errorf(format, args...)
}

// kindOf returns the kind of the key number id.
func (r *modelRun) kindOf(id int) int {
	if r.mixed {
		return id % modelKeyKinds
	}
	return r.kind
}

func (r *modelRun) set(s *subject, kind, id int) {
	if r.failed || len(s.model.list) >= maxModelSteps {
		return
	}
	ident := modelKey{
		kind: kind,
		id:   id,
	}
	k := r.keys.newKey(kind, id)
	_, present := s.model.indexOfKey[ident]
	if !present && len(s.model.indexOfKey) >= r.maxKeys {
		return
	}
	r.value++
	v := float64(r.value)
	// Append is for the keys that a table does not have.
	if !present && r.value%3 != 0 {
		s.table.Append(k, engine.Number(v))
	} else {
		s.table.Set(k, engine.Number(v))
	}
	s.model.set(ident, v)
	// A probe without an empty slot would not end.
	if err := collection.CheckSlotRoom(s.table); err != nil {
		r.errorf("after setting %s: %v", ident, err)
	}
}

func (r *modelRun) delete(s *subject, kind, id int) {
	if r.failed {
		return
	}
	ident := modelKey{
		kind: kind,
		id:   id,
	}
	k := r.keys.newKey(kind, id)
	if got, want := s.table.Delete(k), s.model.delete(ident); got != want {
		r.errorf("Delete(%s) = %t, want %t", ident, got, want)
	}
}

// deleteLive deletes the live entry at the index i of the list of the model.
func (r *modelRun) deleteLive(s *subject, i int) {
	r.delete(s, s.model.list[i].key.kind, s.model.list[i].key.id)
}

func (r *modelRun) deleteFront(s *subject, n int) {
	for i := s.model.skip(0); n > 0 && i < len(s.model.list); i, n = s.model.skip(i+1), n-1 {
		r.deleteLive(s, i)
	}
}

func (r *modelRun) advance(s *subject, c *modelCursor, what string) {
	if r.failed {
		return
	}
	c.pos = s.model.skip(c.pos)
	k, v, ok := s.table.Next(&c.cursor)
	if c.pos == len(s.model.list) {
		if ok {
			r.errorf("%s: an entry with the value %v after the last one", what, v.Number())
		}
		return
	}
	e := &s.model.list[c.pos]
	c.pos++
	if !ok {
		r.errorf("%s: no entry, want %s", what, e.key)
		return
	}
	want := r.keys.newKey(e.key.kind, e.key.id)
	if !engine.SameValueZero(k, want) || !v.IsNumber() || v.Number() != e.value {
		r.errorf("%s: not the entry %s with the value %v", what, e.key, e.value)
	}
}

// check compares s with its model.
func (r *modelRun) check(s *subject) {
	if r.failed {
		return
	}
	if err := collection.CheckInvariants(s.table); err != nil {
		r.errorf("%v", err)
		return
	}
	if got, want := s.table.Size(), len(s.model.indexOfKey); got != want {
		r.errorf("Size() = %d, want %d", got, want)
	}
	// A new cursor sees every live entry in order, and lookups agree with it.
	c := modelCursor{
		cursor: s.table.Start(),
	}
	for c.pos < len(s.model.list) && !r.failed {
		r.advance(s, &c, "iteration")
	}
	for i := range s.model.list {
		e := &s.model.list[i]
		if e.dead {
			continue
		}
		k := r.keys.newKey(e.key.kind, e.key.id)
		if v, ok := s.table.Get(k); !ok || v.Number() != e.value {
			r.errorf("Get(%s) = %v, %t; want %v", e.key, v.Number(), ok, e.value)
			return
		}
	}
	// An open cursor gives the next entry that its model does, without moving.
	for i, c := range s.cursors {
		r.advance(s, &c, fmt.Sprint("cursor ", i))
	}
}

// step executes one operation.
func (r *modelRun) step() {
	op := r.next()
	s := r.subjects[0]
	if op&0xc0 == 0xc0 {
		s = r.subjects[1+op>>5&1]
	}
	arg := r.next()
	what := ""
	switch op & 0x1f {
	case 0, 1:
		what = "set"
		r.set(s, r.kindOf(arg), arg)
	case 2:
		what = "lookup"
		ident := modelKey{
			kind: r.kindOf(arg),
			id:   arg,
		}
		k := r.keys.newKey(ident.kind, ident.id)
		_, want := s.model.indexOfKey[ident]
		if got := s.table.Has(k); got != want {
			r.errorf("Has(%s) = %t, want %t", ident, got, want)
		}
	case 3, 4:
		what = "delete"
		r.delete(s, r.kindOf(arg), arg)
	case 5, 6, 7:
		what = "insert a run"
		for range arg {
			r.set(s, r.kindOf(r.fresh), r.fresh)
			r.fresh++
		}
	case 8:
		what = "insert a run of one kind"
		kind := r.next() % modelKeyKinds
		for range arg {
			r.set(s, kind, r.fresh)
			r.fresh++
		}
	case 9, 10, 11:
		what = "delete at the front"
		r.deleteFront(s, arg)
	case 12:
		what = "delete at the back"
		for i := len(s.model.list) - 1; i >= 0 && arg > 0; i-- {
			if !s.model.list[i].dead {
				r.deleteLive(s, i)
				arg--
			}
		}
	case 13, 14:
		what = "delete some"
		every := arg%7 + 2
		for i := s.model.skip(0); i < len(s.model.list); i = s.model.skip(i + 1) {
			if i%every != 0 {
				r.deleteLive(s, i)
			}
		}
	case 15:
		what = "delete all"
		r.deleteFront(s, len(s.model.list))
	case 16, 17:
		what = "queue"
		for range arg {
			r.set(s, r.kindOf(r.fresh), r.fresh)
			r.fresh++
			r.deleteFront(s, 1)
		}
	case 18:
		what = "insert until full"
		// The last insertion that fits, so that the next one makes room.
		for n := collection.EntryStorage(s.table); s.table.Size() < n && !r.failed; {
			before := s.table.Size()
			r.set(s, r.kindOf(r.fresh), r.fresh)
			r.fresh++
			if s.table.Size() == before {
				break
			}
		}
	case 19:
		what = "reserve"
		s.table.Reserve(min(arg*(1+op>>5&1*4), r.maxKeys))
	case 20:
		what = "trim"
		s.table.Trim()
	case 21:
		what = "clear"
		if arg < 64 {
			s.table.Clear()
			s.model.clear()
		}
	case 22, 23:
		what = "copy"
		dst := r.subjects[arg%modelTables]
		if dst == s {
			break
		}
		r.deleteFront(dst, len(dst.model.list))
		if r.failed || len(dst.model.list)+len(s.model.indexOfKey) > maxModelSteps {
			break
		}
		dst.table.CopyFrom(s.table, nil)
		dst.model.copyFrom(&s.model)
		r.check(dst)
	case 24:
		what = "clone"
		dst := newSubject(s.table.Clone(nil))
		dst.model.copyFrom(&s.model)
		r.subjects[arg%modelTables] = dst
		r.check(dst)
	case 25, 26:
		what = "open a cursor"
		c := modelCursor{
			cursor: s.table.Start(),
		}
		if len(s.cursors) < modelCursors {
			s.cursors = append(s.cursors, c)
		} else {
			s.cursors[arg%modelCursors] = c
		}
	case 27, 28, 29:
		what = "advance a cursor"
		if len(s.cursors) > 0 {
			c := &s.cursors[arg%len(s.cursors)]
			for range r.next() {
				r.advance(s, c, "open cursor")
			}
		}
	default:
		what = "delete during an iteration"
		// A loop that removes the entries it visits and adds others.
		c := modelCursor{
			cursor: s.table.Start(),
		}
		for range arg {
			c.pos = s.model.skip(c.pos)
			r.advance(s, &c, "deleting cursor")
			if c.pos == 0 || s.model.skip(c.pos-1) != c.pos-1 {
				break
			}
			r.deleteLive(s, c.pos-1)
			r.set(s, r.kindOf(r.fresh), r.fresh)
			r.fresh++
		}
	}
	r.check(s)
	if r.failed {
		r.t.Logf("failing operation: %s (%d, %d), %d bytes left", what, op, arg, len(r.ops))
	}
}

// runModel executes the operations of ops, whose first byte selects the hashes and the kinds of
// the keys.
func runModel(t *testing.T, ops []byte) {
	if len(ops) > maxModelBytes {
		t.Skip("too many operations")
	}
	cx, _ := engine.NewExecutionContext(nil)
	r := &modelRun{
		t:   t,
		ops: ops,
		keys: keySource{
			cx:      cx,
			objects: map[int]engine.Value{},
		},
		maxKeys: maxModelKeys,
	}
	config := r.next()
	seeds := modelSeeds[config%len(modelSeeds)]
	if seeds[1] == 0 {
		r.maxKeys = maxCollidingKeys
	}
	defer collection.SetHashSeeds(seeds[0], seeds[1])()
	kind := config / len(modelSeeds) % (2 * modelKeyKinds)
	r.mixed = kind >= modelKeyKinds
	r.kind = kind % modelKeyKinds
	for i := range r.subjects {
		r.subjects[i] = newSubject(&collection.Table{})
	}
	for len(r.ops) > 0 && !r.failed {
		r.step()
	}
}

// randomOps returns n bytes of operations from a seed.
func randomOps(seed uint64, n int) []byte {
	rnd := rand.New(rand.NewPCG(seed, 0x7ab1e))
	ops := make([]byte, n)
	for i := range ops {
		ops[i] = byte(rnd.Uint32())
	}
	ops[0] = byte(seed)
	return ops
}

func TestTableModel(t *testing.T) {
	// The seeds cover each combination of hashes and kinds of keys.
	for seed := range uint64(len(modelSeeds) * 2 * modelKeyKinds) {
		t.Run(fmt.Sprint("seed=", seed), func(t *testing.T) {
			runModel(t, randomOps(seed, 300))
		})
	}
}

func FuzzTableModel(f *testing.F) {
	for seed := range uint64(4) {
		f.Add(randomOps(seed, 100))
	}
	f.Fuzz(func(t *testing.T, ops []byte) {
		runModel(t, ops)
	})
}
