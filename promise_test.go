// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestNewPromiseAsyncFunction checks that an async function awaiting a promise of the host
// resumes, with the continuation data of its await, when the host resolves the promise with the
// result of work done on another goroutine.
func TestNewPromiseAsyncFunction(t *testing.T) {
	var vm ekuma.VM
	setUpContinuationData(t, &vm)
	type result struct {
		resolver *ekuma.PromiseResolver
		body     string
	}
	results := make(chan result)
	fetch := vm.FuncOf("fetch", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		p, res := vm.NewPromise()
		url := args[0].String()
		go func() {
			results <- result{
				resolver: res,
				body:     "body of " + url,
			}
		}()
		return p, nil
	})
	if err := vm.Global().Set("fetch", fetch); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `
var got;
run("A", async () => {
	const body = await fetch("x");
	got = body + " in " + current();
});`)

	r := <-results
	if err := r.resolver.Resolve(t.Context(), r.body); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := run(t, &vm, `got`), "body of x in A"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPromiseResolverReject(t *testing.T) {
	var vm ekuma.VM
	p, res := vm.NewPromise()
	if err := vm.Global().Set("p", p); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `
var reason = new TypeError("boom");
var caught;
(async () => {
	try {
		await p;
	} catch (e) {
		caught = e;
	}
})();`)
	reason, err := vm.Global().Get("reason")
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Reject(t.Context(), reason); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got := run(t, &vm, `caught === reason`); got != "true" {
		t.Errorf("caught === reason: got %s, want true", got)
	}
}

// TestPromiseResolverThenable checks that a thenable is adopted, and that the promise, once
// resolved with it, ignores later calls while it is still pending.
func TestPromiseResolverThenable(t *testing.T) {
	var vm ekuma.VM
	p, res := vm.NewPromise()
	run(t, &vm, `var settle; var thenable = { then(f) { settle = f; } };`)
	thenable, err := vm.Global().Get("thenable")
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Resolve(t.Context(), thenable); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := res.Reject(t.Context(), "second"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if st, _ := p.PromiseState(); st != ekuma.PromisePending {
		t.Errorf("after the second call: got %v, want pending", st)
	}
	run(t, &vm, `settle(42)`)
	if st, v := p.PromiseState(); st != ekuma.PromiseFulfilled || v.Int() != 42 {
		t.Errorf("after the thenable settled: got %v %v, want fulfilled 42", st, v)
	}
}

// TestRealmNewPromise checks that the promise and the TypeError of resolving it with itself
// belong to the realm of the resolver.
func TestRealmNewPromise(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	p, res := r.NewPromise()
	if err := res.Resolve(t.Context(), p); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	st, reason := p.PromiseState()
	if st != ekuma.PromiseRejected {
		t.Fatalf("got %v, want rejected", st)
	}
	for _, c := range []struct {
		v    ekuma.Value
		name string
	}{
		{
			v:    p,
			name: "Promise",
		},
		{
			v:    reason,
			name: "TypeError",
		},
	} {
		ctor, err := r.Global().Get(c.name)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := c.v.InstanceOf(ctor); err != nil || !ok {
			t.Errorf("%v instanceof %s of the realm: got %v, %v, want true", c.v, c.name, ok, err)
		}
	}
}

// TestPromiseResolverInterrupt checks the state of a promise after ctx stops the JavaScript
// that resolving it runs.
func TestPromiseResolverInterrupt(t *testing.T) {
	errStop := errors.New("stop")
	setUp := func(t *testing.T) (*ekuma.VM, context.Context, ekuma.Value, *ekuma.PromiseResolver) {
		vm := &ekuma.VM{}
		ctx, cancel := context.WithCancelCause(t.Context())
		t.Cleanup(func() {
			cancel(nil)
		})
		stop := vm.FuncOf("stop", func(this ekuma.Value, args []ekuma.Value) (any, error) {
			cancel(errStop)
			return nil, nil
		})
		if err := vm.Global().Set("stop", stop); err != nil {
			t.Fatal(err)
		}
		p, res := vm.NewPromise()
		if err := vm.Global().Set("p", p); err != nil {
			t.Fatal(err)
		}
		return vm, ctx, p, res
	}

	t.Run("then getter", func(t *testing.T) {
		vm, ctx, p, res := setUp(t)
		thenable, err := runScript(t.Context(), vm, "thenable", `({ get then() { stop(); for (;;); } })`)
		if err != nil {
			t.Fatal(err)
		}
		if err := res.Resolve(ctx, thenable); !errors.Is(err, errStop) {
			t.Errorf("Resolve: got %v, want %v", err, errStop)
		}
		if err := res.Resolve(t.Context(), 1); err != nil {
			t.Errorf("later Resolve: %v", err)
		}
		if st, _ := p.PromiseState(); st != ekuma.PromisePending {
			t.Errorf("got %v, want pending", st)
		}
	})

	t.Run("reaction", func(t *testing.T) {
		vm, ctx, p, res := setUp(t)
		run(t, vm, `var ran = false; p.then(() => { stop(); for (;;); }); p.then(() => { ran = true; });`)
		if err := res.Resolve(ctx, 1); !errors.Is(err, errStop) {
			t.Errorf("Resolve: got %v, want %v", err, errStop)
		}
		if st, v := p.PromiseState(); st != ekuma.PromiseFulfilled || v.Int() != 1 {
			t.Errorf("got %v %v, want fulfilled 1", st, v)
		}
		// The interruption discards the pending reaction job.
		if got := run(t, vm, `ran`); got != "false" {
			t.Errorf("ran: got %s, want false", got)
		}
	})

	t.Run("done before the call", func(t *testing.T) {
		_, _, p, res := setUp(t)
		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(errStop)
		if err := res.Resolve(ctx, 1); !errors.Is(err, errStop) {
			t.Errorf("Resolve: got %v, want %v", err, errStop)
		}
		if st, _ := p.PromiseState(); st != ekuma.PromisePending {
			t.Errorf("got %v, want pending", st)
		}
		if err := res.Resolve(t.Context(), 2); err != nil {
			t.Errorf("later Resolve: %v", err)
		}
		if st, v := p.PromiseState(); st != ekuma.PromiseFulfilled || v.Int() != 2 {
			t.Errorf("after the later call: got %v %v, want fulfilled 2", st, v)
		}
	})
}

func TestPromiseResolverUnhandledRejection(t *testing.T) {
	var vm ekuma.VM
	var got []string
	var promises []ekuma.Value
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		promises = append(promises, promise)
		got = append(got, reason.String())
	})

	// The rejection is reported before Reject returns.
	p, res := vm.NewPromise()
	if err := res.Reject(t.Context(), "a"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if !slices.Equal(got, []string{"a"}) || !promises[0].Equal(p) {
		t.Errorf("got %q for %v, want [a] for the promise", got, promises)
	}

	// A handler added by a job before the reports is in time.
	got = nil
	p, res = vm.NewPromise()
	if err := vm.Global().Set("p", p); err != nil {
		t.Fatal(err)
	}
	reject := vm.FuncOf("reject", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return nil, res.Reject(context.Background(), args[0])
	})
	if err := vm.Global().Set("reject", reject); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `(async () => { reject("b"); await 0; p.catch(() => {}); })();`)
	if got != nil {
		t.Errorf("got %q, want none", got)
	}
}

func TestPromiseJobCreatesSecondRealm(t *testing.T) {
	var vm ekuma.VM
	var entered bool
	newHandler := vm.FuncOf("newHandler", func(ekuma.Value, []ekuma.Value) (any, error) {
		r := vm.NewRealm()
		return r.FuncOf("probe", func(ekuma.Value, []ekuma.Value) (any, error) {
			entered = ekuma.IsEnteredRealm(r)
			return nil, nil
		}), nil
	})
	if err := vm.Global().Set("newHandler", newHandler); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `Promise.resolve().then(() => { Promise.resolve().then(newHandler()); })`)
	if !entered {
		t.Error("the next job did not enter the realm created during the drain")
	}
}

func TestPromiseJobRestoresEnteredRealm(t *testing.T) {
	var vm ekuma.VM
	drainRealm := vm.NewRealm()
	enqueue, err := runScript(t.Context(), &vm, "enqueue", `() => Promise.resolve().then(() => { throw 42; })`)
	if err != nil {
		t.Fatal(err)
	}
	// The host reports the rejection after the job and before leaving drainRealm.
	var restored bool
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		restored = ekuma.IsEnteredRealm(drainRealm)
	})
	if err := ekuma.InvokeInRealm(t.Context(), drainRealm, enqueue); err != nil {
		t.Fatal(err)
	}
	if !restored {
		t.Error("expected the rejection report in the realm that drained the job")
	}
}

func TestUnhandledRejectionRecoveredInterruption(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("stop in rejection handler")
	canceled, cancel := context.WithCancelCause(t.Context())
	cancel(cause)

	var calls int
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		calls++
		if calls == 1 {
			defer func() { _ = recover() }()
			_, _ = vm.Global().Call(canceled, "eval", "1")
		}
	})
	s, err := vm.ParseScript("test", `Promise.reject("a"); Promise.reject("b"); 42`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Evaluate(t.Context()); !errors.Is(err, cause) {
		t.Errorf("Evaluate: got %v, want %v", err, cause)
	}
	if calls != 1 {
		t.Errorf("callback calls: got %d, want 1", calls)
	}
}
