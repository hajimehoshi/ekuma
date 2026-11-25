// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	_ "embed"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// shortcutProbes defines the operations that can stand for calls of built-in functions without a
// registered shortcut.
//
//go:embed testdata/shortcut-probes.js
var shortcutProbes string

// probeRealm is a realm in which the shortcut probes run.
type probeRealm struct {
	cx *engine.ExecutionContext

	// watching is set while calls of the watched functions count.
	watching bool
	// starts is the number of calls of the functions that each probe stands for since the start
	// of the running one.
	starts map[string]int
}

// newProbeRealm returns a realm in which every probe makes the generic calls if generic is set.
func newProbeRealm(t *testing.T, generic bool) *probeRealm {
	t.Helper()
	r := &probeRealm{
		cx:     newTestContext(t),
		starts: map[string]int{},
	}
	r.eval(t, "var generic = "+strconv.FormatBool(generic)+", setWatching")
	set := r.cx.NewFunction("setWatching", func(cx *engine.ExecutionContext, _ engine.Value, args []engine.Value) engine.Value {
		old := r.watching
		r.watching = storage.ToBoolean(args[0])
		return storage.Bool(old)
	})
	if _, err := r.cx.HostCall(r.eval(t, "f => { setWatching = f; }"), storage.Undefined, []engine.Value{set}); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{kernelProbes, shortcutProbes} {
		if _, err := runScript(r.cx, "probes.js", src); err != nil {
			t.Fatal(err)
		}
	}
	for _, probe := range r.keys(t, "probes") {
		for i := range r.keys(t, "watched("+strconv.Quote(probe)+")") {
			f := r.eval(t, "watched("+strconv.Quote(probe)+")["+strconv.Itoa(i)+"]")
			engine.ObserveNative(f, func() {
				if r.watching {
					r.starts[probe]++
				}
			})
		}
	}
	return r
}

func (r *probeRealm) eval(t *testing.T, src string) engine.Value {
	t.Helper()
	v, err := runScript(r.cx, "eval.js", src)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (r *probeRealm) evalString(t *testing.T, src string) string {
	t.Helper()
	return string(r.cx.ToString(r.eval(t, src)))
}

// keys returns the property names of the value of src.
func (r *probeRealm) keys(t *testing.T, src string) []string {
	t.Helper()
	s := r.evalString(t, "Object.keys("+src+").join('\\n')")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// run runs a site of a probe with an operand and returns a description of what happened.
func (r *probeRealm) run(t *testing.T, probe, site, operand string) string {
	t.Helper()
	clear(r.starts)
	r.eval(t, "shortcutProbe("+strconv.Quote(probe)+", "+strconv.Quote(site)+", "+strconv.Quote(operand)+")")
	if err := r.cx.RunJobs(); err != nil {
		t.Fatal(err)
	}
	result := r.evalString(t, "settled()")
	if site == "tail" {
		// The frame of a proper tail call is gone, and that of a call through a proxy is not.
		result = tailFrames.ReplaceAllString(result, "")
	}
	if probe == "Promise" {
		// Scripts test whether the resolving functions show frames.
		result = resolvingFrames.ReplaceAllString(result, "")
	}
	// A proxy as the receiver has another name.
	return receiverNames.ReplaceAllString(result, "  ")
}

var (
	// tailFrames matches what shows the frame of a function that makes a tail call.
	tailFrames = regexp.MustCompile(`(?m) from (site|native|other)$|^  (tail|site) \(.*\n`)

	// resolvingFrames matches the frames of functions without names.
	resolvingFrames = regexp.MustCompile(`(?m)^  <anonymous>$\n`)

	// receiverNames matches the names of the receivers that can be proxies.
	receiverNames = regexp.MustCompile(`(?m)^  (?:Array|Object|Function|Promise)\.`)
)

// TestUnregisteredShortcuts compares operations that stand for calls of built-in functions without
// a registered shortcut with the same operations where every function that they stand for is a
// proxy of itself, so that it is called.
func TestUnregisteredShortcuts(t *testing.T) {
	shortcut, generic := newProbeRealm(t, false), newProbeRealm(t, true)
	// Some operands leave a realm whose arrays take fewer shortcuts, and run in such a realm.
	var spoiled *probeRealm
	spoiling := map[string]bool{}
	for _, probe := range shortcut.keys(t, "probes") {
		t.Run(probe, func(t *testing.T) {
			p := "probes[" + strconv.Quote(probe) + "]"
			watches := len(shortcut.keys(t, "watched("+strconv.Quote(probe)+")")) > 0
			option := func(name string) *regexp.Regexp {
				return regexp.MustCompile(shortcut.evalString(t, "("+p+"."+name+" || /^$/).source"))
			}
			declined, skipped := option("declined"), option("skipped")
			for _, site := range shortcut.keys(t, p+".sites") {
				for _, operand := range shortcut.keys(t, p+".operands") {
					name := site + " of " + operand
					if skipped.MatchString(name) {
						continue
					}
					r := shortcut
					if spoiling[operand] {
						r = spoiled
					}
					got := r.run(t, probe, site, operand)
					if r == shortcut && !engine.ArraysPlain(r.cx) {
						spoiling[operand] = true
						spoiled, shortcut = r, newProbeRealm(t, false)
						if !engine.ArraysPlain(shortcut.cx) {
							t.Fatal("the arrays of a new realm are not plain")
						}
					}
					want := generic.run(t, probe, site, operand)
					if got != want {
						t.Errorf("%s gave\n%s\nthe generic operation gave\n%s", name, got, want)
					}
					if !watches || declined.MatchString(name) {
						continue
					}
					if n := r.starts[probe]; n > 0 {
						t.Errorf("%s: the shortcut was not taken: %d calls", name, n)
					}
					if generic.starts[probe] == 0 {
						t.Errorf("%s: the generic operation made no call", name)
					}
				}
			}
		})
	}
}

// interrupted runs f with a function that requests an interruption and args, and reports whether
// the interruption stopped it.
func (r *probeRealm) interrupted(t *testing.T, f engine.Value, args ...engine.Value) bool {
	t.Helper()
	defer r.cx.ClearInterrupt()
	_, err := r.cx.HostCall(f, storage.Undefined, args)
	if _, ok := errors.AsType[*engine.Error](err); err != nil && !ok && !errors.Is(err, errKernelInterrupt) {
		t.Fatal(err)
	}
	return errors.Is(err, errKernelInterrupt)
}

// TestUnregisteredShortcutInterruption tests that operations that stand for calls of built-in
// functions without a registered shortcut honor interruptions where the calls do.
func TestUnregisteredShortcutInterruption(t *testing.T) {
	for _, generic := range []bool{false, true} {
		r := newProbeRealm(t, generic)
		var raising bool
		raise := r.cx.NewFunction("raise", func(cx *engine.ExecutionContext, _ engine.Value, _ []engine.Value) engine.Value {
			cx.Interrupt(errKernelInterrupt)
			return storage.Undefined
		})
		for _, key := range r.keys(t, "pending") {
			r.eval(t, "started = false")
			if !r.interrupted(t, r.eval(t, "pending["+strconv.Quote(key)+"]"), raise) {
				t.Errorf("generic=%t: %s was not interrupted", generic, key)
			}
			if storage.ToBoolean(r.eval(t, "started")) {
				t.Errorf("generic=%t: %s started a function after an interruption", generic, key)
			}
		}
		observed := map[*engine.Object]bool{}
		for _, key := range r.keys(t, "long") {
			op := "long[" + strconv.Quote(key) + "]"
			if f := r.eval(t, "("+op+".start || (() => undefined))()"); f.IsObject() {
				f = r.eval(t, "((f) => originals.get(f) || f)("+op+".start())")
				if !observed[f.Object()] {
					observed[f.Object()] = true
					engine.ObserveNative(f, func() {
						if raising {
							raising = false
							r.cx.Interrupt(errKernelInterrupt)
						}
					})
				}
			}
			source := r.eval(t, "("+op+".prepare || (v => v))("+op+".source())")
			raising = true
			got := r.interrupted(t, r.eval(t, op+".run"), source, raise)
			raising = false
			if !got {
				t.Errorf("generic=%t: %s was not interrupted", generic, key)
			}
		}
	}
}
