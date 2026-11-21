// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

type v8Timeout struct {
	realm    *ekuma.Realm
	callback ekuma.Value
}

type v8EventLoop struct {
	host       *V8
	terminated bool

	// Pending IDs are consecutive, starting at lastRemovedID + 1.
	lastRemovedID int
	pending       []v8Timeout
}

func (q *v8EventLoop) install(r *ekuma.Realm) error {
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

func (q *v8EventLoop) scriptError(ctx context.Context, err error) suiteResult {
	if errors.Is(err, errV8Termination) {
		return q.terminationResult(ctx, false)
	}
	return scriptErrorResult(err)
}

func (q *v8EventLoop) terminationResult(ctx context.Context, scriptCompleted bool) suiteResult {
	q.terminated = true
	if err := context.Cause(ctx); err != nil {
		return scriptErrorResult(err)
	}
	// ExecuteSource treats termination caught after Script::Run returned a
	// value as a failed checkpoint. Termination of the script itself succeeds.
	if scriptCompleted {
		return suiteFailed.result("d8 terminated during the script's microtask checkpoint")
	}
	return suiteResult{}
}

func (q *v8EventLoop) drain(ctx context.Context, result suiteResult) suiteResult {
	for !q.terminated && len(q.pending) != 0 && result.outcome != suiteBroken {
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
		if err := q.host.InvokeInRealm(ctx, task.realm, task.callback); err != nil {
			failure := q.scriptError(ctx, err)
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
					// Flags: --ignore-unhandled-promises
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
	t.Run("onerror", func(t *testing.T) {
		checkout := fstest.MapFS{
			"callback.js": {
				Data: []byte("try {\n/*😀*/ throw token;\n} finally { try { throw 0; } catch {} }"),
			},
			test.file: {
				Data: []byte(`
					const realm = Realm.createAllowCrossRealmAccess();
					const target = Realm.global(realm);
					const token = { toString() { order += "S"; return "token"; } };
					let order = "";
					let calls = 0;
					if (target.onerror !== null) throw Error("initial handler");
					target.onerror = function(message, source, line, column, ex) {
						"use strict";
						if (arguments.length !== 5 || this !== target || Realm.current() !== realm ||
						    message !== "Uncaught token" || source !== "callback.js" ||
						    line !== 2 || column !== 8 || ex !== token) {
							print(message, source, line, column);
							quit(1);
						}
						order += "H";
						Promise.resolve().then(() => order += "M");
						switch (++calls) {
						case 1: return true;
						case 2: return 1;
						default: throw Error("handler failed");
						}
					};
					for (let i = 0; i < 3; i++) target.setTimeout(() => load("callback.js"));
					setTimeout(() => print(order));`),
			},
		}
		result, printed := test.execute(t.Context(), host, checkout, testTimeout)
		if result.outcome != suiteFailed {
			t.Errorf("outcome = %v, want a timer failure", result.outcome)
		}
		if want := "[SHMSHSMSHSM]"; printed != want {
			t.Errorf("last print = %q, want %q", printed, want)
		}
	})
	t.Run("proxy exception description", func(t *testing.T) {
		checkout := fstest.MapFS{
			test.file: {
				Data: []byte(`
					const traps = {get() { quit(1); }};
					const error = Object.setPrototypeOf(new Error("own message"), new Proxy({}, traps));
					const ex = new Proxy(error, traps);
					onerror = (message, source, line, column, thrown) => {
						if (thrown === ex) print(message);
						return true;
					};
					setTimeout(() => { throw ex; });`),
			},
		}
		_, printed := test.execute(t.Context(), host, checkout, testTimeout)
		if want := "[Uncaught own message]"; printed != want {
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

		file := checkout[test.file]
		file.Data = append([]byte("// Flags: --ignore-unhandled-promises\n"), file.Data...)
		result, _ = withHarness.execute(t.Context(), host, checkout, testTimeout)
		if result.outcome != suitePassed {
			t.Error(result.reason)
		}
	})
}
