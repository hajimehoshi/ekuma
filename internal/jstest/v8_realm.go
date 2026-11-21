// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	_ "embed"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma"
)

//go:embed testdata/harness/v8-realm.js
var v8RealmSource string

var v8RealmScript = sync.OnceValues(func() (*ekuma.UnboundScript, error) {
	return ekuma.ParseUnboundScript("v8-realm.js", v8RealmSource)
})

type v8Realms struct {
	host     *V8
	vm       *ekuma.VM
	ctx      context.Context
	checkout fs.FS
	print    ekuma.Func
	entries  []ekuma.Value
}

func (h *v8Realms) add(r *ekuma.Realm) (int, error) {
	s, err := v8RealmScript()
	if err != nil {
		return 0, err
	}
	init, err := r.BindScript(s).Evaluate(h.ctx)
	if err != nil {
		return 0, err
	}
	if err := r.Global().Set("print", r.FuncOf("print", h.print)); err != nil {
		return 0, err
	}
	if err := installV8ScriptLoader(h.ctx, r, h.checkout); err != nil {
		return 0, err
	}
	create := r.FuncOf("create", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return h.add(h.vm.NewRealm())
	})
	lookup := r.FuncOf("lookup", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		i := args[0].Int()
		if i < 0 || i >= len(h.entries) {
			return ekuma.Undefined, nil
		}
		return h.entries[i], nil
	})
	view, err := init.Invoke(h.ctx, create, lookup)
	if err != nil {
		return 0, err
	}
	evaluate := r.FuncOf("evaluate", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		s, err := h.host.ParseScriptValue(r, "(d8)", args[0])
		if se, ok := errors.AsType[*ekuma.SyntaxError](err); ok {
			// d8 compiles the script before entering the selected realm.
			e, err := args[1].New(h.ctx, se.Message)
			if err != nil {
				return nil, err
			}
			return nil, &ekuma.ScriptError{
				Value: e,
			}
		}
		if err != nil {
			return nil, err
		}
		return s.Evaluate(h.ctx)
	})
	index := len(h.entries)
	h.entries = append(h.entries, r.ValueOf(map[string]any{
		"global":   h.host.InstallGlobalProxy(r, view),
		"evaluate": evaluate,
	}))
	return index, nil
}

// CheckRealm checks script evaluation and global object access across V8 realms.
func (host *V8) CheckRealm(t *testing.T) {
	const source = `
		const r = Realm.create();
		if (Realm.eval(r, 'd8.file.execute("helper.js"); loadedIn') !== Realm.global(r)) {
			throw Error("script loaded in the wrong realm");
		}
		const unit = "\ud800";
		let readArguments = false;
		const options = {type: "string", get arguments() { readArguments = true; }};
		if (Realm.eval(r, "'" + unit + "'", options) !== unit) throw Error("source string changed");
		if (!readArguments) throw Error("source arguments getter was skipped");

		let syntaxError;
		try { Realm.eval(r, "let = ;"); } catch (e) { syntaxError = e; }
		if (!(syntaxError instanceof SyntaxError)) throw Error("wrong syntax error realm");

		const receiver = Realm.eval(r, 'Object.defineProperty(this, "self", {get() { return this }}); () => self');
		if (receiver() !== Realm.global(r)) throw Error("wrong global getter receiver");
		if (Reflect.setPrototypeOf(globalThis, globalThis)) throw Error("global prototype cycle");
	`
	test := v8Test{
		file: "test/mjsunit/realm.js",
	}
	checkout := fstest.MapFS{
		"helper.js": {
			Data: []byte("var loadedIn = globalThis"),
		},
		test.file: {
			Data: []byte(source),
		},
	}
	if result := test.run(t.Context(), host, checkout, testTimeout); result.outcome != suitePassed {
		t.Error(result.reason)
	}
}
