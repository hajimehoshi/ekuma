// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// The test binary cannot capture the output of an example in a browser.
//go:build !js

package ekuma_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma"
)

// This example runs a module that imports a module and a JSON module from a file system.
func Example_modules() {
	fsys := fstest.MapFS{
		"main.js": {Data: []byte(`
			import { greet } from "./lib/greet.js";
			import config from "./config.json" with { type: "json" };
			export const message = greet(config.name);`)},
		"lib/greet.js": {Data: []byte(`
			export function greet(name) { return "Hello, " + name; }`)},
		"config.json": {Data: []byte(`{"name": "modules"}`)},
	}

	var vm ekuma.VM
	modules := map[string]*ekuma.Module{}
	load := func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		// A specifier is relative to the importing module.
		name := path.Join(path.Dir(req.ReferrerName), req.Specifier)
		if m, ok := modules[name]; ok {
			return m, nil
		}
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var m *ekuma.Module
		if req.Attributes["type"] == "json" {
			m, err = vm.ParseJSONModule(name, string(src))
		} else {
			m, err = vm.ParseModule(name, string(src))
		}
		if err != nil {
			return nil, err
		}
		modules[name] = m
		return m, nil
	}
	vm.SetModuleLoader(load)

	m, err := load(ekuma.ModuleRequest{Specifier: "main.js"})
	if err != nil {
		panic(err)
	}
	// Evaluate loads and links the modules first.
	p, err := m.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	state, _ := p.PromiseState()
	fmt.Println(state)

	ns, err := m.Namespace()
	if err != nil {
		panic(err)
	}
	msg, err := ns.Get("message")
	if err != nil {
		panic(err)
	}
	fmt.Println(msg.String())

	// Output:
	// fulfilled
	// Hello, modules
}

// This example imports a Go function and a value from a synthetic module.
func ExampleVM_NewSyntheticModule() {
	var vm ekuma.VM
	native := vm.NewSyntheticModule("native", map[string]ekuma.Value{
		"appName": vm.ValueOf("Ekuma"),
		"greet": vm.FuncOf("greet", func(_ ekuma.Value, _ []ekuma.Value) (any, error) {
			return "Hello", nil
		}),
	})
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		if req.Specifier != "native" {
			return nil, fmt.Errorf("unknown module %q", req.Specifier)
		}
		return native, nil
	})

	main, err := vm.ParseModule("main.js", `
		import { appName, greet } from "native";
		export const message = greet() + ", " + appName;
	`)
	if err != nil {
		panic(err)
	}
	p, err := main.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	if state, reason := p.PromiseState(); state != ekuma.PromiseFulfilled {
		panic(fmt.Errorf("module evaluation %v: %v", state, reason))
	}

	ns, err := main.Namespace()
	if err != nil {
		panic(err)
	}
	message, err := ns.Get("message")
	if err != nil {
		panic(err)
	}
	fmt.Println(message.String())

	// Output: Hello, Ekuma
}

// This example parses a script once and evaluates it several times.
func ExampleVM_ParseScript() {
	var vm ekuma.VM
	s, err := vm.ParseScript("count.js", `globalThis.n = (globalThis.n ?? 0) + 1`)
	if err != nil {
		fmt.Println(err)
		return
	}
	for range 3 {
		v, err := s.Evaluate(context.Background())
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(v.Int())
	}
	// Output:
	// 1
	// 2
	// 3
}

func ExampleVM_NewRealm() {
	var vm ekuma.VM
	r := vm.NewRealm()
	if err := vm.Global().Set("other", r.Global()); err != nil {
		fmt.Println(err)
		return
	}
	s, err := vm.ParseScript("main.js", `
		Array.prototype.first = function() { return this[0]; };
		[typeof [].first, typeof other.eval("[]").first, other.Array === Array].join()`)
	if err != nil {
		fmt.Println(err)
		return
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(v)
	// Output: function,undefined,false
}

// This example hands the memory of a SharedArrayBuffer to a VM on another goroutine, whose
// store the first VM reads after Atomics.wait: the wait blocks until the notification, or
// returns at once when the store came first.
func Example_sharedArrayBuffer() {
	ctx := context.Background()
	blocks := make(chan *ekuma.SharedDataBlock)
	done := make(chan error)
	go func() {
		var worker ekuma.VM
		if err := worker.Global().Set("sab", <-blocks); err != nil {
			done <- err
			return
		}
		s, err := worker.ParseScript("worker.js", `
			const ia = new Int32Array(sab);
			Atomics.store(ia, 0, 42);
			Atomics.notify(ia, 0);`)
		if err != nil {
			done <- err
			return
		}
		_, err = s.Evaluate(ctx)
		done <- err
	}()

	var vm ekuma.VM
	vm.SetCanBlock(true)
	main, err := vm.ParseScript("main.js", `globalThis.ia = new Int32Array(new SharedArrayBuffer(4)); ia.buffer`)
	if err != nil {
		fmt.Println(err)
		return
	}
	sab, err := main.Evaluate(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	blocks <- sab.SharedDataBlock()
	wait, err := vm.ParseScript("wait.js", `Atomics.wait(ia, 0, 0); Atomics.load(ia, 0)`)
	if err != nil {
		fmt.Println(err)
		return
	}
	v, err := wait.Evaluate(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := <-done; err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(v.Int())
	// Output: 42
}

func ExampleVM_FuncOf() {
	var vm ekuma.VM
	uppercase := vm.FuncOf("uppercase", func(_ ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) != 1 || args[0].Type() != ekuma.TypeString {
			return nil, errors.New("uppercase expects one string")
		}
		return strings.ToUpper(args[0].String()), nil
	})
	if err := vm.Global().Set("uppercase", uppercase); err != nil {
		panic(err)
	}

	s, err := vm.ParseScript("greeting.js", `"Hello, " + uppercase("Ekuma")`)
	if err != nil {
		panic(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(v.String())

	// Output: Hello, EKUMA
}

func ExampleValue_Invoke() {
	ctx := context.Background()
	var vm ekuma.VM
	s, err := vm.ParseScript("total.js", `(order) => order.price * order.quantity`)
	if err != nil {
		panic(err)
	}
	total, err := s.Evaluate(ctx)
	if err != nil {
		panic(err)
	}
	v, err := total.Invoke(ctx, map[string]any{
		"price":    12,
		"quantity": 3,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(v.Int())

	// Output: 36
}

func ExampleParseUnboundScript() {
	s, err := ekuma.ParseUnboundScript("greeting.js", `"Hello, " + name`)
	if err != nil {
		panic(err)
	}

	for _, name := range []string{"Alice", "Bob"} {
		var vm ekuma.VM
		if err := vm.Global().Set("name", name); err != nil {
			panic(err)
		}
		v, err := vm.BindScript(s).Evaluate(context.Background())
		if err != nil {
			panic(err)
		}
		fmt.Println(v.String())
	}

	// Output:
	// Hello, Alice
	// Hello, Bob
}

func ExampleVM_NewPromise() {
	ctx := context.Background()
	var vm ekuma.VM
	promise, resolver := vm.NewPromise()
	if err := vm.Global().Set("message", promise); err != nil {
		panic(err)
	}
	s, err := vm.ParseScript("greeting.js", `message.then((name) => "Hello, " + name)`)
	if err != nil {
		panic(err)
	}
	greeting, err := s.Evaluate(ctx)
	if err != nil {
		panic(err)
	}
	state, _ := greeting.PromiseState()
	fmt.Println(state)

	if err := resolver.Resolve(ctx, "Ekuma"); err != nil {
		panic(err)
	}
	state, value := greeting.PromiseState()
	fmt.Println(state, value.String())

	// Output:
	// pending
	// fulfilled Hello, Ekuma
}

func ExampleVM_NewArrayBuffer() {
	var vm ekuma.VM
	pixels := []byte{10, 20, 30, 255}
	if err := vm.Global().Set("pixels", vm.NewArrayBuffer(pixels)); err != nil {
		panic(err)
	}
	s, err := vm.ParseScript("invert.js", `
		const rgba = new Uint8Array(pixels);
		for (let i = 0; i < 3; i++) {
			rgba[i] = 255 - rgba[i];
		}
	`)
	if err != nil {
		panic(err)
	}
	if _, err := s.Evaluate(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println(pixels)

	// Output: [245 235 225 255]
}

func ExampleScriptError() {
	var vm ekuma.VM
	s, err := vm.ParseScript("order.js", `throw { code: "OUT_OF_STOCK" }`)
	if err != nil {
		panic(err)
	}
	_, err = s.Evaluate(context.Background())
	exception, ok := errors.AsType[*ekuma.ScriptError](err)
	if !ok {
		panic(fmt.Sprintf("expected a JavaScript exception, got %v", err))
	}
	code, err := exception.Value.Get("code")
	if err != nil {
		panic(err)
	}
	fmt.Println(code.String())

	// Output: OUT_OF_STOCK
}
