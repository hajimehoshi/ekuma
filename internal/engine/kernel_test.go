// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// kernelProbes defines the operands that the shortcuts are tested with.
//
//go:embed testdata/kernel-probes.js
var kernelProbes string

// The call sites of both ways to call a built-in function have the same names and positions. A
// call of a function through call takes the shortcuts, and Reflect.apply, which shows no frame,
// makes the generic call.
const (
	shortcutSites = `
function site(f, t) { return f            .call(t); }
function site(f, t, x) { return f            .call(t, x); }
function site(f, t, x, y) { return f            .call(t, x, y); }`
	genericSites = `
function site(f, t) { return Reflect      .apply(f, t, []); }
function site(f, t, x) { return Reflect      .apply(f, t, [x]); }
function site(f, t, x, y) { return Reflect      .apply(f, t, [x, y]); }`
	shortcutNewSites = `
function site(f, t) { return               new f(); }
function site(f, t, x) { return               new f(x); }
function site(f, t, x, y) { return               new f(x, y); }`
	genericNewSites = `
function site(f, t) { return Reflect      .construct(f, []); }
function site(f, t, x) { return Reflect      .construct(f, [x]); }
function site(f, t, x, y) { return Reflect      .construct(f, [x, y]); }`
	// The interruption is requested after the last check before the call.
	pendingSites = `
function site(f, raise, t) { f.call((raise(), t)); }
function site(f, raise, t, x) { f.call((raise(), t), x); }
function site(f, raise, t, x, y) { f.call((raise(), t), x, y); }`
	// The function that a bound function calls reaches its kernel on another way than through call.
	boundSites = `
function site(f, t) { return      f.bind(t)(); }
function site(f, t, x) { return      f.bind(t)(x); }
function site(f, t, x, y) { return      f.bind(t)(x, y); }`
	// The interpreter hands the calls of a method %[1]s that it does not run in place to the kernel
	// without a frame.
	methodSites = `
function site(f, t) { return t            .%[1]s(); }
function site(f, t, x) { return t            .%[1]s(x); }`
	pendingMethodSites = `
function site(f, raise, t) { (raise(), t).%[1]s(); }
function site(f, raise, t, x) { (raise(), t).%[1]s(x); }`
)

// recordBuiltins is called while the built-in objects of a realm are created, when it is not nil.
var recordBuiltins func(cx *engine.ExecutionContext)

func init() {
	builtin.RegisterExtension(func(cx *engine.ExecutionContext) {
		if recordBuiltins != nil {
			recordBuiltins(cx)
		}
	})
}

// kernelTest is a realm whose shortcuts are observed.
type kernelTest struct {
	cx        *engine.ExecutionContext
	shortcuts []*engine.Shortcut

	// subject is the shortcut under test, and events what its calls from site did during the
	// last run.
	subject *engine.Shortcut
	site    storage.Value
	events  []string

	// raise makes the first start of the subject request an interruption.
	raise bool
}

var errKernelInterrupt = errors.New("interrupted in a shortcut")

func newKernelTest(t *testing.T) *kernelTest {
	t.Helper()
	k := &kernelTest{}
	var holders []*storage.Object
	recordBuiltins = func(cx *engine.ExecutionContext) {
		holders = engine.BuiltinHolders(cx)
	}
	defer func() {
		recordBuiltins = nil
	}()
	k.cx = newTestContext(t)
	if len(holders) == 0 {
		t.Fatal("no holders of built-in functions")
	}
	k.shortcuts = engine.Shortcuts(t, k.cx, holders, func(s *engine.Shortcut, form, event string) {
		if s != k.subject || engine.RunningFunction(k.cx).Object() != k.site.Object() {
			return
		}
		k.events = append(k.events, form+" "+event)
		if event == "start" && k.raise {
			k.raise = false
			k.cx.Interrupt(errKernelInterrupt)
		}
	})
	if len(k.shortcuts) == 0 {
		t.Fatal("no shortcuts")
	}
	if _, err := runScript(k.cx, "kernel-probes.js", kernelProbes); err != nil {
		t.Fatal(err)
	}
	return k
}

// eval returns the value of the script src.
func (k *kernelTest) eval(t *testing.T, src string) storage.Value {
	t.Helper()
	v, err := runScript(k.cx, "eval.js", src)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return v
}

// keys returns the property names of the global object name.
func (k *kernelTest) keys(t *testing.T, name string) []string {
	t.Helper()
	return strings.Split(string(k.cx.ToString(k.eval(t, "Object.keys("+name+").join('\\n')"))), "\n")
}

// sites returns the functions of the lines of src, each of which is a script of the same name.
func (k *kernelTest) sites(t *testing.T, src string) []storage.Value {
	t.Helper()
	var sites []storage.Value
	for line := range strings.Lines(strings.TrimSpace(src)) {
		v, err := runScript(k.cx, "site.js", "("+line+")")
		if err != nil {
			t.Fatal(err)
		}
		sites = append(sites, v)
	}
	return sites
}

func (k *kernelTest) array(strs []string) storage.Value {
	var vals []storage.Value
	for _, s := range strs {
		vals = append(vals, k.cx.StringFromGo(s))
	}
	return k.cx.NewArrayOf(vals)
}

// run calls f with site, the function of s and args. It records what the calls of the function
// from site do.
func (k *kernelTest) run(s *engine.Shortcut, f, site storage.Value, args ...storage.Value) (storage.Value, error) {
	k.subject, k.site, k.events = s, site, nil
	defer func() {
		k.subject, k.raise = nil, false
		k.cx.ClearInterrupt()
	}()
	return k.cx.HostCall(f, storage.Undefined, append([]storage.Value{site, s.Func}, args...))
}

// probe is run for a function f that returns a description.
func (k *kernelTest) probe(t *testing.T, s *engine.Shortcut, f, site storage.Value, args ...storage.Value) string {
	t.Helper()
	v, err := k.run(s, f, site, args...)
	if err != nil {
		t.Fatal(err)
	}
	return string(k.cx.ToString(v))
}

// interrupted reports whether an interruption that is requested as the function of s or its form
// starts stops the run.
func (k *kernelTest) interrupted(t *testing.T, s *engine.Shortcut, f, site storage.Value, args ...storage.Value) bool {
	t.Helper()
	k.raise = true
	_, err := k.run(s, f, site, args...)
	if _, ok := errors.AsType[*storage.Error](err); err != nil && !ok && !errors.Is(err, errKernelInterrupt) {
		t.Fatal(err)
	}
	return errors.Is(err, errKernelInterrupt)
}

// formArgs is the number of arguments of the calls that take a form.
var formArgs = map[string]int{
	"method0":  0,
	"method1":  1,
	"method2":  2,
	"number1":  1,
	"number2":  2,
	"extremum": 2,
}

// observed reports whether a probe that returned result ran a hook.
func observed(result string) bool {
	return !strings.HasPrefix(result, "0 hooks")
}

// operandLists returns the operands that functions of argc arguments are tested with: every this
// value and argument of the operands, where only a few are arguments in combinations with others.
func operandLists(operands []string, argc int, usesThis bool) [][]string {
	const few = 8
	receivers := operands
	if !usesThis {
		receivers = operands[:1]
	}
	var lists [][]string
	for _, this := range receivers {
		switch argc {
		case 0:
			lists = append(lists, []string{this})
		case 1:
			for _, x := range operands {
				lists = append(lists, []string{this, x})
			}
		case 2:
			for i, x := range operands {
				for j, y := range operands {
					if i < few && j < few || !usesThis && (i < few || j < few) {
						lists = append(lists, []string{this, x, y})
					}
				}
			}
		}
	}
	return lists
}

// TestKernels tests the contracts of the shortcuts of the built-in functions, and compares what
// they do with what the calls of the functions do.
func TestKernels(t *testing.T) {
	k := newKernelTest(t)
	for _, s := range k.shortcuts {
		t.Run(s.Name, func(t *testing.T) {
			k.testContract(t, s)
			// The clock stands still for Date.now.
			synctest.Test(t, func(t *testing.T) {
				for _, form := range s.Forms {
					_, calls := formArgs[form]
					switch {
					case form == "intrinsic":
						k.testIntrinsic(t, s)
					case calls:
						k.testCalls(t, s, form)
					default:
						k.testSteps(t, s, form)
					}
				}
			})
		})
	}
}

func (k *kernelTest) testContract(t *testing.T, s *engine.Shortcut) {
	if len(s.Forms) == 0 {
		t.Error("no forms")
	}
	if s.Contract.Frame == engine.FrameUndeclared {
		t.Error("the contract does not state how the frame of the call is accounted for")
	}
	if s.Contract.Pace == engine.PaceUndeclared {
		t.Error("the contract does not state how interruptions are accounted for")
	}
	if s.Frameless {
		t.Error("the function shows no frame, unlike its contract says")
	}
}

// took reports whether form stood for the call of the subject, which did not start then.
func (k *kernelTest) took(form string) bool {
	if form == "extremum" {
		// The interpreter computes it without calling anything.
		return len(k.events) == 0
	}
	return slices.Contains(k.events, form+" taken")
}

// declined reports whether form left the call to the native function of the subject.
func (k *kernelTest) declined(form string) bool {
	return !k.took(form) && slices.Contains(k.events, "native start")
}

// testCalls compares the calls of s that can take form with the generic calls.
func (k *kernelTest) testCalls(t *testing.T, s *engine.Shortcut, form string) {
	argc := formArgs[form]
	probe := k.eval(t, "probe")
	shortcut := k.sites(t, shortcutSites)[argc]
	generic := k.sites(t, genericSites)[argc]
	if form == "number2" && slices.Contains(s.Forms, "extremum") || strings.HasPrefix(form, "method") && slices.Contains(s.Forms, "intrinsic") {
		// The interpreter runs the calls through call itself.
		shortcut = k.sites(t, boundSites)[argc]
	}

	var taken, declined int
	var first []string
	compare := func(keys []string) {
		t.Helper()
		got := k.probe(t, s, probe, shortcut, k.array(keys))
		took := k.took(form)
		if took {
			if slices.Contains(k.events, "native start") {
				t.Errorf("%s%q: the function ran after the kernel", form, keys)
			}
			taken++
			if first == nil {
				first = keys
			}
			if s.Contract.Frame == engine.FrameUnobservable && observed(got) {
				t.Errorf("%s%q: code ran in a kernel whose contract says it runs none:\n%s", form, keys, got)
			}
		} else if k.declined(form) {
			declined++
		} else {
			t.Errorf("%s%q: neither the kernel nor the function ran: %v", form, keys, k.events)
		}
		want := k.probe(t, s, probe, generic, k.array(keys))
		if !slices.Equal(k.events, []string{"native start"}) {
			t.Errorf("%s%q: the generic call did not call the function alone: %v", form, keys, k.events)
		}
		if got != want {
			t.Errorf("%s%q: the call gave\n%s\nthe generic call gave\n%s", form, keys, got, want)
		}
	}

	usesThis := strings.HasPrefix(form, "method")
	lists := operandLists(k.keys(t, "operands"), argc, usesThis)
	for _, keys := range lists {
		compare(keys)
	}
	if taken == 0 {
		t.Fatalf("no operands take the %s kernel: add some to kernel-probes.js", form)
	}
	if declined == 0 && form != "method0" {
		t.Errorf("no operands make the %s kernel decline: add some to kernel-probes.js", form)
	}
	k.testPending(t, s, form, pendingSites, first)
	k.testPace(t, s, form, shortcut, generic)
	k.withHostHooks(t, func() {
		for _, keys := range lists {
			if !slices.ContainsFunc(keys[1:], func(key string) bool { return key != "one" }) {
				compare(keys)
			}
		}
	})
	if !k.cx.IsConstructor(s.Func.Object()) {
		return
	}
	shortcut, generic = k.sites(t, shortcutNewSites)[argc], k.sites(t, genericNewSites)[argc]
	for _, keys := range operandLists(k.keys(t, "operands"), argc, false) {
		got := k.probe(t, s, probe, shortcut, k.array(keys))
		if !slices.Equal(k.events, []string{"native start"}) {
			t.Errorf("new %q: the function did not run alone: %v", keys, k.events)
		}
		if want := k.probe(t, s, probe, generic, k.array(keys)); got != want {
			t.Errorf("new %q gave\n%s\nReflect.construct gave\n%s", keys, got, want)
		}
	}
}

// hostHooks are a time zone and a random source that call a function of a realm.
type hostHooks struct {
	call func(name string)
}

func (h *hostHooks) Offset(float64) (float64, string) {
	h.call("time zone")
	return 0, ""
}

func (h *hostHooks) Uint64() uint64 {
	h.call("random source")
	return 1
}

// withHostHooks calls f while the hooks of the host call functions written in JavaScript.
func (k *kernelTest) withHostHooks(t *testing.T, f func()) {
	t.Helper()
	newHook := k.eval(t, "name => hook(name)")
	h := &hostHooks{
		call: func(name string) {
			hook := k.cx.Call(newHook.Object(), storage.Undefined, k.cx.StringFromGo(name))
			k.cx.Call(hook.Object(), storage.Undefined)
		},
	}
	k.cx.SetClock(func() time.Time {
		h.call("clock")
		return time.Unix(1, 0)
	})
	k.cx.SetDefaultTimeZone(h)
	k.cx.SetRandomSource(h)
	defer func() {
		k.cx.SetClock(nil)
		k.cx.SetDefaultTimeZone(nil)
		k.cx.SetRandomSource(nil)
	}()
	f()
}

// testPending tests that a call of a site of src that takes form for the operands keys honors an
// interruption that was requested before it.
func (k *kernelTest) testPending(t *testing.T, s *engine.Shortcut, form, src string, keys []string) {
	site := k.sites(t, src)[len(keys)-1]
	raise := k.cx.NewFunction("raise", func(cx *engine.ExecutionContext, _ storage.Value, _ []storage.Value) storage.Value {
		cx.Interrupt(errKernelInterrupt)
		return storage.Undefined
	})
	f := k.eval(t, "(site, f, keys, raise) => site(f, raise, ...keys.map(operand))")
	if _, err := k.run(s, f, site, k.array(keys), raise); !errors.Is(err, errKernelInterrupt) {
		t.Errorf("%s%q: got error %v after a pending interruption, want %v", form, keys, err, errKernelInterrupt)
	}
	if len(k.events) > 0 {
		t.Errorf("%s%q: the call started after a pending interruption: %v", form, keys, k.events)
	}
}

// testPace tests that the calls of s that take form honor an interruption that is requested while
// they run wherever the generic calls do.
func (k *kernelTest) testPace(t *testing.T, s *engine.Shortcut, form string, shortcut, generic storage.Value) {
	if form == "extremum" {
		// It has no operands that take long, and nothing can interrupt it.
		return
	}
	var polled bool
	f := k.eval(t, "(site, f, keys) => site(f, ...keys.map(k => large[k]))")
	for _, keys := range operandLists(k.keys(t, "large"), formArgs[form], strings.HasPrefix(form, "method")) {
		got := k.interrupted(t, s, f, shortcut, k.array(keys))
		started, left := slices.Contains(k.events, form+" start"), slices.Contains(k.events, form+" declined")
		want := k.interrupted(t, s, f, generic, k.array(keys))
		if got != want {
			t.Errorf("%s%q: the call was interrupted: %t, the generic call: %t", form, keys, got, want)
		}
		polled = polled || started && !left && got
	}
	if want := s.Contract.Pace == engine.PacePolled; polled != want {
		t.Errorf("%s: checks for an interruption: %t, want %t by the contract", form, polled, want)
	}
}

// testIntrinsic compares the calls of s that the interpreter runs itself, in place or by the
// kernel of s, with the generic calls, for the receivers of kernel-probes.js.
func (k *kernelTest) testIntrinsic(t *testing.T, s *engine.Shortcut) {
	i := slices.IndexFunc(s.Forms, func(form string) bool { return strings.HasPrefix(form, "method") })
	if i < 0 {
		t.Fatal("no kernel")
	}
	kernel := s.Forms[i]
	argc := formArgs[kernel]
	method := s.Name[strings.LastIndexByte(s.Name, '.')+1:]
	k.setOtherRealmArray(t)
	// Each site has a generic site of its own, whose calls of push leave the same hints for the
	// storage of later arrays.
	newCompare := func(name, src string) func(keys []string) []string {
		site, generic := k.sites(t, src)[argc], k.sites(t, genericSites)[argc]
		return func(keys []string) []string {
			t.Helper()
			events, genericEvents := k.compareIntrinsic(t, name, s, site, generic, keys)
			if !slices.Equal(genericEvents, []string{"native start"}) {
				t.Errorf("%s%q: the generic call did not call the function alone: %v", name, keys, genericEvents)
			}
			return events
		}
	}
	compareCall, compareMethod := newCompare("call", shortcutSites), newCompare("method", fmt.Sprintf(methodSites, method))

	const (
		inPlace    = "ran in place"
		toKernel   = "went to the kernel"
		toFunction = "went to the function"
	)
	// The numbers of the calls through call that went each way, and of those that were also made as
	// methods.
	calls, methods := map[string]int{}, map[string]int{}
	var first []string
	for _, key := range k.keys(t, "receivers") {
		asMethodToo := k.hasMethod(t, key, s.Func, method)
		for _, keys := range receiverLists(key, argc) {
			var way string
			switch events := compareCall(keys); {
			case len(events) == 0:
				way = inPlace
			case slices.Equal(events, []string{kernel + " start", kernel + " taken"}):
				way = toKernel
			case slices.Equal(events, []string{kernel + " start", kernel + " declined", "native start"}):
				way = toFunction
			default:
				t.Errorf("call%q: the call went neither in place nor through the kernel once: %v", keys, events)
				continue
			}
			calls[way]++
			if !asMethodToo {
				continue
			}
			methods[way]++
			if way == inPlace && first == nil {
				first = keys
			}
			// The interpreter calls the kernel itself, which leaves no events.
			var want []string
			if way == toFunction {
				want = []string{"native start"}
			}
			if events := compareMethod(keys); !slices.Equal(events, want) {
				t.Errorf("method%q: the call, which %s through call, did %v", keys, way, events)
			}
		}
	}
	for _, way := range []string{inPlace, toKernel, toFunction} {
		if calls[way] == 0 {
			t.Errorf("no receivers whose calls %s: add some to kernel-probes.js", way)
		}
		if methods[way] == 0 {
			t.Errorf("no receivers with the method whose calls %s: add some to kernel-probes.js", way)
		}
	}
	if first != nil {
		k.testPending(t, s, "method", fmt.Sprintf(pendingMethodSites, method), first)
	}

	if argc == 0 {
		// Only additions of elements consult the prototypes.
		return
	}
	// Each change spoils its realm, whose functions are not observed.
	for _, change := range k.keys(t, "prototypeChanges") {
		r := &kernelTest{
			cx: k.cx.CreateRealm(builtin.Install),
		}
		if _, err := runScript(r.cx, "kernel-probes.js", kernelProbes); err != nil {
			t.Fatal(err)
		}
		r.eval(t, "prototypeChanges["+strconv.Quote(change)+"]()")
		f := &engine.Shortcut{
			Func: r.eval(t, "Array.prototype."+method),
		}
		call, generic := r.sites(t, shortcutSites)[argc], r.sites(t, genericSites)[argc]
		for _, keys := range receiverLists("array up to the accessor", argc) {
			r.compareIntrinsic(t, change+": call", f, call, generic, keys)
		}
	}
}

// setOtherRealmArray sets otherRealmArray of kernel-probes.js.
func (k *kernelTest) setOtherRealmArray(t *testing.T) {
	t.Helper()
	newArray, err := runScript(k.cx.CreateRealm(builtin.Install), "realm.js", `n => { var a = new Array(n + 10).fill("x"); a.length = n; return a; }`)
	if err != nil {
		t.Fatal(err)
	}
	set := k.eval(t, "f => { otherRealmArray = () => f(accessorIndex); accessor(Object.getPrototypeOf(f(0))); }")
	if _, err := k.cx.HostCall(set, storage.Undefined, []storage.Value{newArray}); err != nil {
		t.Fatal(err)
	}
}

// hasMethod reports whether the receiver key of kernel-probes.js has f as its property name.
func (k *kernelTest) hasMethod(t *testing.T, key string, f storage.Value, name string) bool {
	t.Helper()
	v, err := k.cx.HostCall(k.eval(t, "hasMethod"), storage.Undefined, []storage.Value{k.cx.StringFromGo(key), f, k.cx.StringFromGo(name)})
	if err != nil {
		t.Fatal(err)
	}
	return storage.ToBoolean(v)
}

// receiverLists returns the operands of the calls of argc arguments with the receiver key: push
// gets a number and a string.
func receiverLists(key string, argc int) [][]string {
	if argc == 0 {
		return [][]string{{key}}
	}
	return [][]string{{key, "one"}, {key, "two units"}}
}

// compareIntrinsic compares the call of s at site with the operands keys with the generic call, and
// returns what the calls of s from each did.
func (k *kernelTest) compareIntrinsic(t *testing.T, name string, s *engine.Shortcut, site, generic storage.Value, keys []string) (events, genericEvents []string) {
	t.Helper()
	probe := k.eval(t, "probe")
	got := k.probe(t, s, probe, site, k.array(keys)) + k.receiverStorage(t)
	events = k.events
	want := k.probe(t, s, probe, generic, k.array(keys)) + k.receiverStorage(t)
	if got != want {
		t.Errorf("%s%q: the call gave\n%s\nthe generic call gave\n%s", name, keys, got, want)
	}
	return events, k.events
}

// receiverStorage describes the storage of the elements of the this value of the last probe.
func (k *kernelTest) receiverStorage(t *testing.T) string {
	t.Helper()
	v := k.eval(t, "lastOperands[0]")
	if !v.IsObject() {
		return ""
	}
	return fmt.Sprintf("\nstorage for %d elements", engine.ElementStorage(v))
}

// testSteps compares the steps of the iterators by the next method s through form with the calls
// of s.
func (k *kernelTest) testSteps(t *testing.T, s *engine.Shortcut, form string) {
	var site storage.Value
	switch form {
	case "arrayIndex":
		site = k.eval(t, "indexedArraySite")
		probe := k.eval(t, "probeIndexedArray")
		got := k.probe(t, s, probe, site, storage.False)
		if !k.took(form) {
			t.Errorf("indexed array getter did not take %s: %v", form, k.events)
		}
		want := k.probe(t, s, probe, site, storage.True)
		if !k.declined(form) || got != want {
			t.Errorf("indexed array getter gave %s, generic call gave %s with %v", got, want, k.events)
		}
		if !k.interrupted(t, s, probe, site, storage.False) || !k.interrupted(t, s, probe, site, storage.True) {
			t.Error("indexed array getter did not honor the interruption")
		}
		return
	case "step", "framedStep":
		site = k.eval(t, "stepSite")
	case "pairStep":
		site = k.eval(t, "pairSite")
	default:
		t.Fatalf("no test for the form %s", form)
	}
	probe := k.eval(t, "probeIterator")
	var taken, hooks int
	for _, key := range k.keys(t, "iterators") {
		it := k.cx.StringFromGo(key)
		got := k.probe(t, s, probe, site, it, storage.False)
		if !slices.Contains(k.events, form+" start") {
			t.Errorf("%s of %s: the form did not start: %v", form, key, k.events)
		}
		if k.took(form) {
			taken++
			if observed(got) {
				hooks++
			}
		}
		want := k.probe(t, s, probe, site, it, storage.True)
		if slices.ContainsFunc(k.events, func(e string) bool { return e != "native start" }) || len(k.events) == 0 {
			t.Errorf("%s of %s: the generic step did not call next alone: %v", form, key, k.events)
		}
		if got != want {
			t.Errorf("%s of %s gave\n%s\nthe call of next gave\n%s", form, key, got, want)
		}
	}
	if taken == 0 {
		t.Errorf("no iterators take the form %s: add some to kernel-probes.js", form)
	}
	if want := s.Contract.Frame == engine.FrameRecorded; hooks > 0 != want {
		t.Errorf("%s: ran code for %d iterators, which the contract allows: %t", form, hooks, want)
	}

	var polled bool
	f := k.eval(t, "(site, next, key, generic) => site(stepped(largeIterators[key](), next, generic))")
	for _, key := range k.keys(t, "largeIterators") {
		it := k.cx.StringFromGo(key)
		if _, err := k.run(s, f, site, it, storage.False); err != nil || slices.Contains(k.events, "native start") {
			// The iterator is not of the kind of next.
			continue
		}
		got := k.interrupted(t, s, f, site, it, storage.False)
		// The step that was interrupted did not end.
		polled = polled || got && !slices.Contains(k.events, form+" taken") && !slices.Contains(k.events, form+" declined")
		if want := k.interrupted(t, s, f, site, it, storage.True); got != want {
			t.Errorf("%s of %s was interrupted: %t, the call of next: %t", form, key, got, want)
		}
	}
	if want := s.Contract.Pace == engine.PacePolled; polled != want {
		t.Errorf("%s: checks for an interruption: %t, want %t by the contract", form, polled, want)
	}
}
