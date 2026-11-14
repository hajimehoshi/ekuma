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
