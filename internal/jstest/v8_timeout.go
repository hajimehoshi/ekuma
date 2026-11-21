// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

type v8Timeout struct {
	realm    *ekuma.Realm
	callback ekuma.Value
}

type v8Timeouts struct {
	invoke func(context.Context, *ekuma.Realm, ekuma.Value) error

	// Pending IDs are consecutive, starting at lastRemovedID + 1.
	lastRemovedID int
	pending       []v8Timeout
}

func (q *v8Timeouts) install(r *ekuma.Realm) error {
	setTimeout := r.FuncOf("setTimeout", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) == 0 || args[0].Type() != ekuma.TypeFunction {
			return 0, nil
		}
		// Plain d8 posts a foreground task regardless of the delay.
		q.pending = append(q.pending, v8Timeout{
			realm:    r,
			callback: args[0],
		})
		return q.lastRemovedID + len(q.pending), nil
	})
	if err := r.Global().Set("setTimeout", setTimeout); err != nil {
		return err
	}
	clearTimeout := r.FuncOf("clearTimeout", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) != 0 && args[0].Type() == ekuma.TypeNumber {
			id := int(numconv.ToInt32(args[0].Float()))
			if id > q.lastRemovedID && id-q.lastRemovedID <= len(q.pending) {
				q.pending[id-q.lastRemovedID-1] = v8Timeout{}
			}
		}
		return ekuma.Undefined, nil
	})
	return r.Global().Set("clearTimeout", clearTimeout)
}

func (q *v8Timeouts) drain(ctx context.Context, result suiteResult) suiteResult {
	for len(q.pending) != 0 && result.outcome != suiteBroken {
		if err := context.Cause(ctx); err != nil {
			return scriptErrorResult(err)
		}
		task := q.pending[0]
		q.pending[0] = v8Timeout{}
		q.pending = q.pending[1:]
		q.lastRemovedID++
		if task.realm == nil {
			continue
		}
		// Each call drains its promise jobs before the next task runs.
		if err := q.invoke(ctx, task.realm, task.callback); err != nil {
			failure := scriptErrorResult(err)
			if result.outcome == suitePassed || failure.outcome == suiteBroken {
				result = failure
			}
		}
	}
	return result
}

// CheckTimeouts checks task ordering, realm access, and the result of a throwing callback.
func (host *V8) CheckTimeouts(t *testing.T) {
	test := v8Test{
		file: "test/mjsunit/timeouts.js",
	}
	t.Run("ordering", func(t *testing.T) {
		checkout := fstest.MapFS{
			"helper.js": {
				Data: []byte(`
					let order = "";
					const child = Realm.create();
					const schedule = Realm.eval(child, "setTimeout");
					const id = schedule(() => { throw Error("cancelled timer ran"); });
					clearTimeout(id);
					const rejected = Promise.reject("handled by a timer");
					schedule(function () {
						"use strict";
						if (this !== undefined || arguments.length) throw Error("timer arguments");
						const peer = Realm.createAllowCrossRealmAccess();
						Realm.eval(child, "Realm.global(" + peer + ").marker = 1");
						order += "T";
						rejected.catch(() => { order += "M"; });
						setTimeout(() => print(order));
					}, 1000, "ignored");`),
			},
			test.file: {
				Data: []byte("// Files: helper.js\norder += 'S'; setTimeout(() => { order += 'U'; });"),
			},
		}
		result, printed := test.execute(t.Context(), host, checkout, testTimeout)
		if result.outcome != suitePassed {
			t.Error(result.reason)
		}
		if want := "[STMU]"; printed != want {
			t.Errorf("last print = %q, want %q", printed, want)
		}
	})
	t.Run("exception", func(t *testing.T) {
		checkout := fstest.MapFS{
			test.file: {
				Data: []byte(`
					setTimeout(() => { throw Error("timer exception"); });
					setTimeout(() => print("continued"));`),
			},
		}
		result, printed := test.execute(t.Context(), host, checkout, testTimeout)
		if result.outcome != suiteFailed {
			t.Errorf("outcome = %v, want failure", result.outcome)
		}
		if want := "[continued]"; printed != want {
			t.Errorf("last print = %q, want %q", printed, want)
		}
	})
	t.Run("module checkpoint", func(t *testing.T) {
		checkout := fstest.MapFS{
			"helper.mjs": {
				Data: []byte(`
					setTimeout(() => { throw Error("timer exception"); });
					await new Promise(resolve => setTimeout(resolve));`),
			},
			test.file: {
				Data: []byte("// Files: helper.mjs\nprint('next file');"),
			},
		}
		result, printed := test.execute(t.Context(), host, checkout, testTimeout)
		if result.outcome != suitePassed {
			t.Error(result.reason)
		}
		if want := "[next file]"; printed != want {
			t.Errorf("last print = %q, want %q", printed, want)
		}
	})
	t.Run("unhandled rejection from harness", func(t *testing.T) {
		harness, err := ekuma.ParseUnboundScript("harness.js", `
			const handledLater = Promise.reject(1);
			Promise.reject(2);`)
		if err != nil {
			t.Fatal(err)
		}
		withHarness := test
		withHarness.helpers = []*ekuma.UnboundScript{harness}
		checkout := fstest.MapFS{
			test.file: {
				Data: []byte(`setTimeout(() => handledLater.catch(() => {}));`),
			},
		}
		result, _ := withHarness.execute(t.Context(), host, checkout, testTimeout)
		if result.outcome != suiteFailed {
			t.Errorf("outcome = %v, want failure", result.outcome)
		}
	})
}
