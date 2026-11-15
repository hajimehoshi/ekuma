// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// The test binary cannot capture the output of an example in a browser.
//go:build !js

package ekuma_test

import (
	"context"
	"fmt"
	"io/fs"
	"path"
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

// This example hands the memory of a SharedArrayBuffer to a VM on another goroutine, whose
// store wakes the Atomics.wait of the first VM.
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
