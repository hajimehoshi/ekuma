// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"reflect"
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// NewExecutionContextWithBigIntResultLimit limits checked BigInt results to maxBits bits.
func NewExecutionContextWithBigIntResultLimit(maxBits int64) *ExecutionContext {
	cx, _ := NewExecutionContext(nil)
	cx.maxBigIntResultBits = maxBits
	return cx
}

// InternedCount returns the number of entries in the table of interned strings of cx.
func InternedCount(cx *ExecutionContext) int {
	return len(cx.strings.interned.entries)
}

// ObjectTransitionCount returns the number of transitions from the empty shape of the objects
// whose prototype is %Object.prototype%.
func ObjectTransitionCount(cx *ExecutionContext) int {
	return cx.realm.shapes.object.transitions.len()
}

// ElementStorage returns the number of values that the storage of the elements of the object
// v has room for, including those that removing its first elements left unused.
func ElementStorage(v Value) int {
	return v.o.elemsStorage()
}

// StackSize returns the number of slots of the stack of cx.
func StackSize(cx *ExecutionContext) int {
	return len(cx.stack)
}

// JobStorage returns the number of jobs that the storage of the job queue of cx has room for.
func JobStorage(cx *ExecutionContext) int {
	return cap(cx.jobs.jobs)
}

// MaxNatives is the number of calls of native functions that can run at once.
const MaxNatives = maxNatives

// TraceShortcut records the distance to the next dispatched instruction of f after each
// execution of its single op site. bypass forces that site to take its generic path.
func TraceShortcut(t *testing.T, f Value, op bytecode.Opcode, bypass bool, distances *[]int) {
	t.Helper()
	fp := f.o.fn.code
	site := -1
	for pc, i := range fp.code {
		if i.Op == op {
			if site >= 0 {
				t.Fatalf("multiple %v sites in %s", op, fp.name)
			}
			site = pc
		}
	}
	if site < 0 {
		t.Fatalf("no %v site in %s", op, fp.name)
	}

	switch op {
	case bytecode.OpCopyPropToGlobal:
		if fp.code[site+1].Op != bytecode.OpGetProp {
			t.Fatal("expected the source property read after OpCopyPropToGlobal")
		}
	case bytecode.OpGetPropChain:
		if fp.code[site+1].Op != bytecode.OpGetProp {
			t.Fatal("expected a second named read after OpGetPropChain")
		}
	case bytecode.OpGetMethod:
		call := fp.code[site+1]
		if call.Op != bytecode.OpCall || call.B != fp.code[site].A {
			t.Fatal("expected a call of the loaded method after OpGetMethod")
		}
	case bytecode.OpIterNextPair:
		if fp.code[site+1].Op != bytecode.OpExtra || fp.code[site+2].Op != bytecode.OpIterNext {
			t.Fatal("expected the extra word and ordinary iterator step after OpIterNextPair")
		}
		if bypass {
			// Give this VM its own copy and keep the offsets of handlers and jumps.
			fp.code = slices.Clone(fp.code)
			jump := bytecode.Instr{
				Op: bytecode.OpJump,
				A:  int32(site + 2),
			}
			fp.code[site], fp.code[site+1] = jump, jump
		}
	default:
		t.Fatalf("unsupported shortcut %v", op)
	}

	var stepping bool
	fp.context.checks.observeSteps(func(current *funcProto, pc int) {
		if current != fp {
			return
		}
		if stepping {
			*distances = append(*distances, pc-site)
		}
		stepping = pc == site
		if stepping && bypass && op == bytecode.OpCopyPropToGlobal {
			store := fp.code[fp.code[site].C-1]
			fp.globals[store.C] = globalCache{}
		}
		if stepping && bypass && (op == bytecode.OpGetPropChain || op == bytecode.OpGetMethod) {
			// A cold lookup dispatches the following read or call separately.
			fp.caches[fp.code[site].C] = propCache{}
		}
	})
}

// ValidateExec reports whether the interpreter checks the code that it runs.
const ValidateExec = validateExec

// Registers is the registers of a running frame, loaded when it was created or last reloaded.
type Registers struct {
	ag   *agent
	fp   *funcProto
	regs []Value
}

// NewRegisters starts a frame of f and loads its registers.
func NewRegisters(f *bytecode.Func) *Registers {
	fp := &funcProto{
		Func:  f,
		code:  f.Code,
		nregs: f.NumRegs,
	}
	ag := &agent{
		frames: []frame{{
			fp: fp,
		}},
	}
	ag.ensureStack(fp.nregs + 1)
	return &Registers{
		ag:   ag,
		fp:   fp,
		regs: ag.stack[:fp.nregs+1],
	}
}

// Step checks the instruction at pc as the interpreter loop does before running it.
func (r *Registers) Step(pc int) {
	r.ag.checkStep(r.fp, pc, r.regs)
}

// EnsureStack makes room on the stack as a call does, without reloading the registers.
func (r *Registers) EnsureStack() {
	r.ag.ensureStack(r.fp.nregs + 1)
}

// Reload loads the registers as the interpreter loop does after a call.
func (r *Registers) Reload() {
	r.regs = r.ag.reload(r.regs, 0, r.fp)
}

// Set stores v in the register i of the registers loaded last.
func (r *Registers) Set(i int, v Value) {
	r.regs[i] = v
}

// Get returns the register i of the registers loaded last.
func (r *Registers) Get(i int) Value {
	return r.regs[i]
}

// CheckDispatchSequence checks successive instruction offsets in a frame of f.
func CheckDispatchSequence(f *bytecode.Func, pcs ...int) {
	fp := &funcProto{
		Func:  f,
		code:  f.Code,
		nregs: f.NumRegs,
	}
	ag := &agent{
		frames: []frame{{
			fp: fp,
		}},
		stack: make([]Value, fp.nregs+1),
	}
	for _, pc := range pcs {
		ag.checkStep(fp, pc, ag.stack)
	}
}

// CorruptCaches makes every filled cache of the function f answer wrongly for the receivers that
// it already answers for.
func CorruptCaches(f Value) {
	fp := f.o.fn.code
	for i := range fp.caches {
		e := &fp.caches[i].cacheEntry
		switch e.kind {
		case cacheOwn:
			e.slot ^= 1
		case cacheProto:
			v := *e.ref
			e.ref = &v
		case cacheGetter, cacheSetter:
			e.holder = f.o
		case cacheAdd:
			e.next = e.shape
		}
	}
	for i := range fp.globals {
		if c := &fp.globals[i]; c.ref != nil {
			v := *c.ref
			c.ref = &v
		}
	}
}

// BuiltinHolders returns the objects that have got a property since the creation of the built-in
// objects of cx started. It is empty once their creation has ended.
func BuiltinHolders(cx *ExecutionContext) []*Object {
	return slices.Clone(cx.builtins.objects)
}

// RunningFunction returns the innermost running function written in JavaScript, or undefined.
func RunningFunction(cx *ExecutionContext) Value {
	if n := len(cx.frames); n > 0 {
		return Value{o: cx.frames[n-1].cl}
	}
	return Undefined
}

// Shortcut is a native function of a realm that has forms that its callers use instead of calling it.
type Shortcut struct {
	// Name is the name of the function, qualified with that of the object that holds it.
	Name string

	Func      Value
	Contract  ShortcutContract
	Frameless bool

	// Forms are the names of the forms.
	Forms []string
}

// Shortcuts returns the native functions with forms that holders, the global object and the
// intrinsics of cx lead to. It calls observe for what happens to them from then on: "start" when
// a form or, as the form "native", the function starts, and "taken" or "declined" when a form ends.
func Shortcuts(t *testing.T, cx *ExecutionContext, holders []*Object, observe func(s *Shortcut, form, event string)) []*Shortcut {
	t.Helper()
	var objects []*Object
	names := map[*Object]string{}
	visit := func(o *Object, name string) {
		if o == nil || !(Value{o: o}).IsObject() || o.class == classProxy {
			return
		}
		if old, ok := names[o]; !ok {
			names[o] = name
			objects = append(objects, o)
		} else if old == "" {
			names[o] = name
		}
	}
	for _, o := range holders {
		visit(o, "")
	}
	visit(cx.realm.global, "")
	for _, o := range cx.realm.intrinsics {
		visit(o, "")
	}
	// The slice grows while it is visited.
	for i := 0; i < len(objects); i++ {
		o := objects[i]
		visit(cx.ProtoOf(o), "")
		for _, k := range cx.OwnKeys(o) {
			if d, ok := cx.GetOwnProperty(o, k); ok {
				name := holderName(cx, o) + string(cx.KeyString(k))
				visit(d.value.o, name)
				visit(d.getter, name)
				visit(d.setter, name)
			}
		}
	}
	var shortcuts []*Shortcut
	for _, o := range objects {
		if o.fn == nil || o.fn.info == nil || o.fn.info.fast == nil && o.fn.info.extremum == ExtremumNone {
			continue
		}
		s := &Shortcut{
			Name:      names[o],
			Func:      Value{o: o},
			Frameless: o.fn.frameless(),
		}
		if s.Name == "" {
			s.Name = string(o.fn.nativeName())
		}
		s.wrap(t, observe)
		shortcuts = append(shortcuts, s)
	}
	return shortcuts
}

// holderName returns the prefix for the names of the properties of the built-in object o.
func holderName(cx *ExecutionContext, o *Object) string {
	if o.fn != nil {
		return string(o.fn.nativeName()) + "."
	}
	if v, ok := cx.lookupData(o, cx.Key(KeyConstructor)); ok && v.o.fn != nil {
		if d, ok := cx.GetOwnProperty(v.o, PropKey{key: cx.Key(KeyPrototype)}); ok && d.value.o == o {
			return string(v.o.fn.nativeName()) + ".prototype."
		}
	}
	if v, ok := cx.lookupData(o, cx.WellKnownSymbol(SymbolToStringTag)); ok && v.IsString() {
		return string(v.Str()) + "."
	}
	return ""
}

// wrap fills in the forms of s and makes them call observe.
func (s *Shortcut) wrap(t *testing.T, observe func(s *Shortcut, form, event string)) {
	t.Helper()
	fn := s.Func.o.fn
	native := fn.native
	fn.native = func(cx *ExecutionContext, this Value, args []Value) Value {
		observe(s, "native", "start")
		return native(cx, this, args)
	}
	if fn.info.extremum != ExtremumNone {
		s.Forms = append(s.Forms, "extremum")
	}
	fast := fn.info.fast
	if fast == nil {
		return
	}
	s.Contract = fast.contract
	ended := func(form string, taken bool) {
		if taken {
			observe(s, form, "taken")
		} else {
			observe(s, form, "declined")
		}
	}
	wrapped := *fast
	for field, value := range reflect.ValueOf(*fast).Fields() {
		if value.Kind() != reflect.Func || value.IsNil() {
			continue
		}
		form := field.Name
		s.Forms = append(s.Forms, form)
		switch form {
		case "step":
			wrapped.step = func(cx *ExecutionContext, this Value) (Value, bool) {
				observe(s, form, "start")
				v, ok := fast.step(cx, this)
				ended(form, true)
				return v, ok
			}
		case "pairStep":
			wrapped.pairStep = func(state any) (Value, Value, PairStep) {
				observe(s, form, "start")
				a, b, st := fast.pairStep(state)
				ended(form, st != PairDeclined)
				return a, b, st
			}
		case "number1":
			wrapped.number1 = func(x float64) float64 {
				observe(s, form, "start")
				defer ended(form, true)
				return fast.number1(x)
			}
		case "number2":
			wrapped.number2 = func(x, y float64) float64 {
				observe(s, form, "start")
				defer ended(form, true)
				return fast.number2(x, y)
			}
		case "method0":
			wrapped.method0 = func(cx *ExecutionContext, this Value) (Value, bool) {
				observe(s, form, "start")
				v, ok := fast.method0(cx, this)
				ended(form, ok)
				return v, ok
			}
		case "method1":
			wrapped.method1 = func(cx *ExecutionContext, this, x Value) (Value, bool) {
				observe(s, form, "start")
				v, ok := fast.method1(cx, this, x)
				ended(form, ok)
				return v, ok
			}
		case "method2":
			wrapped.method2 = func(cx *ExecutionContext, this, x, y Value) (Value, bool) {
				observe(s, form, "start")
				v, ok := fast.method2(cx, this, x, y)
				ended(form, ok)
				return v, ok
			}
		default:
			t.Fatalf("%s has a form %s that the test does not know", s.Name, form)
		}
	}
	fn.info.fast = &wrapped
}
