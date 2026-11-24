// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// warmEvaluations is the number of evaluations in the warm mode. The first one fills the caches
// of the code, the second one meets the state that the first one left, and the third one runs warm.
const warmEvaluations = 3

// observation is what one evaluation of a test script shows to its host.
type observation struct {
	result suiteResult

	// completion describes the completion value or the type of the uncaught error.
	completion observedValue

	// frames are the lines of the stack trace of the uncaught error.
	frames []string

	printed  []string
	asserted []observedValue
}

// difference describes the first difference of got from the observation o of the normal mode,
// and is empty when there is none.
func (o *observation) difference(got *observation) string {
	if o.result.outcome != got.result.outcome {
		return fmt.Sprintf("failed: %s", got.result.reason)
	}
	if !o.completion.equal(got.completion) {
		return fmt.Sprintf("completion: got %s, want %s", got.completion, o.completion)
	}
	if diff := listDifference("assertion", got.asserted, o.asserted, observedValue.equal); diff != "" {
		return diff
	}
	sameText := func(a, b string) bool { return a == b }
	if diff := listDifference("printed line", got.printed, o.printed, sameText); diff != "" {
		return diff
	}
	return listDifference("stack frame", got.frames, o.frames, sameText)
}

func listDifference[T string | observedValue](name string, got, want []T, equal func(T, T) bool) string {
	for i := range max(len(got), len(want)) {
		switch {
		case i == len(got):
			return fmt.Sprintf("%s %d: got nothing, want %q", name, i+1, want[i])
		case i == len(want):
			return fmt.Sprintf("%s %d: got %q, want nothing", name, i+1, got[i])
		case !equal(got[i], want[i]):
			return fmt.Sprintf("%s %d: got %q, want %q", name, i+1, got[i], want[i])
		}
	}
	return ""
}

// recordAssertions makes the assertions of the harness record the values that they check.
func (tv *testVM) recordAssertions() error {
	assert, err := tv.vm.Global().Get("assert")
	if err != nil {
		return err
	}
	sameValue, err := assert.Get("sameValue")
	if err != nil {
		return err
	}
	record := tv.vm.FuncOf("sameValue", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		actual := ekuma.Undefined
		if len(args) > 0 {
			actual = args[0]
		}
		tv.asserted = append(tv.asserted, observeValue(actual))
		rest := make([]any, len(args))
		for i, a := range args {
			rest[i] = a
		}
		return sameValue.Invoke(context.Background(), rest...)
	})
	return assert.Set("sameValue", record)
}

// observedValue keeps strings and BigInts for exact comparison across VMs. Other values are
// represented by their descriptions, so recording an object does not keep it alive.
type observedValue struct {
	text  string
	value ekuma.Value
}

func observeValue(v ekuma.Value) observedValue {
	var text string
	switch v.Type() {
	case ekuma.TypeString, ekuma.TypeBigInt:
		return observedValue{
			value: v,
		}
	case ekuma.TypeNumber:
		text = v.String()
		if f := v.Float(); f == 0 && math.Signbit(f) {
			text = "-0"
		}
	case ekuma.TypeBoolean:
		text = v.String()
	default:
		text = v.Type().String()
	}
	return observedValue{
		text: text,
	}
}

func (v observedValue) equal(w observedValue) bool {
	return v.text == w.text && v.value.Equal(w.value)
}

func (v observedValue) String() string {
	switch v.value.Type() {
	case ekuma.TypeString:
		s := v.value.String()
		if len(s) > maxDiagnosticLineBytes {
			const half = maxDiagnosticLineBytes / 2
			return fmt.Sprintf("%q…%q (%d bytes)", s[:half], s[len(s)-half:], len(s))
		}
		return fmt.Sprintf("%q", s)
	case ekuma.TypeBigInt:
		n := v.value.BigInt()
		if n.BitLen() > 256 {
			return fmt.Sprintf("BigInt (%d bits)", n.BitLen())
		}
		return n.String() + "n"
	}
	return v.text
}

// evaluate evaluates s and returns what it shows.
func (tv *testVM) evaluate(ctx context.Context, s *ekuma.Script) *observation {
	tv.printed = nil
	tv.asserted = nil
	v, err := s.Evaluate(ctx)
	var o observation
	if err == nil {
		o.completion = observeValue(v)
	} else {
		o.completion.text = "throw " + errorName(err)
		if _, ok := errors.AsType[*ekuma.ScriptError](err); ok {
			for line := range strings.SplitSeq(err.Error(), "\n") {
				if strings.HasPrefix(line, "    at ") {
					o.frames = append(o.frames, strings.TrimSpace(line))
				}
			}
		}
	}
	o.result = tv.check(ctx, err)
	o.printed = tv.printed
	o.asserted = tv.asserted
	return &o
}

// observe evaluates src in a new VM the given number of times, until the outcome is not the
// expected one, and returns what each evaluation shows.
func (t *test262Test) observe(h *harness, src string, parse parseFunc, evaluations int, timeout time.Duration) []*observation {
	var observations []*observation
	result := runInVM(h, t.path, t.m, parse, time.Duration(evaluations)*timeout, func(ctx context.Context, tv *testVM) suiteResult {
		s, err := tv.parse(tv.vm.InitialRealm(), t.path, src)
		if err != nil {
			observations = append(observations, &observation{
				completion: observedValue{
					text: "throw " + errorName(err),
				},
				result: tv.check(ctx, err),
			})
			return suiteResult{}
		}
		for range evaluations {
			o := tv.evaluate(ctx, s)
			observations = append(observations, o)
			if o.result.outcome != suitePassed {
				break
			}
		}
		return suiteResult{}
	})
	if result.outcome != suitePassed {
		// The VM could not evaluate the script.
		observations = append(observations, &observation{result: result})
	}
	return observations
}

// runModes runs the test in the normal mode and compares the other modes with it.
func (t *test262Test) runModes(h *harness, timeout time.Duration) suiteResult {
	if t.m.flags["noRepeat"] != (t.m.noRepeatReason != "") {
		return suiteBroken.result("the flag noRepeat and the key noRepeatReason need each other")
	}
	if t.m.flags["module"] {
		// The conservative compiler takes scripts only, and a module evaluates once.
		return t.run(h, timeout)
	}
	for _, strict := range t.strictModes() {
		prefix := ""
		if strict {
			prefix = "strict mode: "
		}
		src := scriptSource(t.src, strict)
		normalObservations := t.observe(h, src, (*ekuma.Realm).ParseScript, 1, timeout)
		normal := normalObservations[len(normalObservations)-1]
		if normal.result.outcome != suitePassed {
			result := normal.result
			result.reason = prefix + result.reason
			return result
		}
		modes := []struct {
			name        string
			src         string
			parse       parseFunc
			evaluations int
		}{
			{
				name:        "conservative mode",
				src:         src,
				parse:       h.suite.ParseConservativeScript,
				evaluations: 1,
			},
			{
				name:        "warm mode",
				src:         scriptSource(repeatable(t.src), strict),
				parse:       (*ekuma.Realm).ParseScript,
				evaluations: warmEvaluations,
			},
		}
		for _, mode := range modes {
			if mode.evaluations > 1 && t.m.flags["noRepeat"] {
				continue
			}
			for i, o := range t.observe(h, mode.src, mode.parse, mode.evaluations, timeout) {
				if diff := normal.difference(o); diff != "" {
					name := prefix + mode.name
					if mode.evaluations > 1 {
						name += fmt.Sprintf(" (evaluation %d of %d)", i+1, mode.evaluations)
					}
					result := suiteFailed.result(name + ": " + diff)
					result.retry = o.result.retry
					return result
				}
			}
		}
	}
	return suiteResult{}
}

// repeatable returns the script src in a form that can be evaluated repeatedly. A block takes a
// script with lexical declarations, which a realm accepts only once. The block moves no line.
func repeatable(src string) string {
	prog, err := parser.ParseScript(wtf8.FromValidUTF8(src), parser.Options{})
	if err != nil {
		return src
	}
	var start int32
	if strings.HasPrefix(src, "#!") && len(prog.Body) > 0 {
		start = prog.Body[0].Start()
	}
	for _, s := range prog.Body {
		if e, ok := s.(*ast.ExprStmt); ok && e.Directive {
			start = e.End()
			continue
		}
		lexical := false
		switch s := s.(type) {
		case *ast.VarDecl:
			lexical = s.Kind != ast.DeclVar
		case *ast.ClassDecl:
			lexical = true
		}
		if lexical {
			// The last directive can end with an implicit semicolon.
			return string(prog.Source[:start]) + ";{" + string(prog.Source[start:]) + "\n}"
		}
	}
	return src
}

// CheckScriptModes checks that a divergence of a mode from the normal run fails a local script.
func (suite *Test262) CheckScriptModes(t *testing.T) {
	const parseNegative = "/*---\nnegative:\n  phase: parse\n  type: SyntaxError\n---*/\nlet = ;"
	const strictDirective = `#!/usr/bin/env js
		"use strict"
		let receiver = (function () { return this; })();
		assert.sameValue(receiver, undefined);`
	for _, tt := range []struct {
		name string
		src  string

		// conservative is the source text that the conservative compiler gets instead of src.
		conservative string

		// want is the prefix of the reason of the failure.
		want string
	}{
		{
			name:         "no divergence",
			src:          `let a = 1; assert.sameValue(a, 1); a;`,
			conservative: `let a = 1; assert.sameValue(a, 1); a;`,
		},
		{
			name:         "expected parse error",
			src:          parseNegative,
			conservative: parseNegative,
		},
		{
			name:         "strict directive in warm mode",
			src:          strictDirective,
			conservative: strictDirective,
		},
		{
			name:         "bigint completion",
			src:          `1n;`,
			conservative: `2n;`,
			want:         `conservative mode: completion: `,
		},
		{
			name:         "asserted string beyond the displayed prefix",
			src:          `let s = "x".repeat(200) + "a"; assert.sameValue(s, s);`,
			conservative: `let s = "x".repeat(200) + "b"; assert.sameValue(s, s);`,
			want:         `conservative mode: assertion 1: `,
		},
		{
			name:         "completion",
			src:          `1;`,
			conservative: `2;`,
			want:         `conservative mode: completion: `,
		},
		{
			name:         "assertion",
			src:          `assert.sameValue(0, 0);`,
			conservative: ``,
			want:         `conservative mode: assertion 1: `,
		},
		{
			name:         "printed line",
			src:          `print("a"); print("b");`,
			conservative: `print("a"); print("c");`,
			want:         `conservative mode: printed line 2: `,
		},
		{
			name:         "stack frame",
			src:          "/*---\nnegative:\n  phase: runtime\n  type: Error\n---*/\nthrow new Error();",
			conservative: "/*---\nnegative:\n  phase: runtime\n  type: Error\n---*/\n\nthrow new Error();",
			want:         `conservative mode: stack frame 1: `,
		},
		{
			name:         "second evaluation",
			src:          `let n = globalThis.n; globalThis.n = 1; assert.sameValue(n, undefined);`,
			conservative: `let n = globalThis.n; globalThis.n = 1; assert.sameValue(n, undefined);`,
			want:         `warm mode (evaluation 2 of 3): failed: `,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := *suite
			s.ParseConservativeScript = func(r *ekuma.Realm, name, _ string) (*ekuma.Script, error) {
				return r.ParseScript(name, tt.conservative)
			}
			h := s.newHarness(localTest262HarnessFS{})
			h.recordAssertions = true
			test := test262Test{
				path: "modes.js",
				src:  tt.src,
				m:    parseMetadata(tt.src),
			}
			test.m.flags["noStrict"] = true
			result := test.runModes(h, testTimeout)
			if passed := result.outcome == suitePassed; passed != (tt.want == "") || !strings.HasPrefix(result.reason, tt.want) {
				t.Errorf("reason = %q, want the prefix %q", result.reason, tt.want)
			}
		})
	}
}
