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
	shell    v8Shell
	entries  []ekuma.Value
}

func (h *v8Realms) add(r *ekuma.Realm, shareAccess bool) (int, error) {
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
	if err := h.shell.install(h.ctx, r, h.checkout); err != nil {
		return 0, err
	}
	create := r.FuncOf("create", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return h.add(h.vm.NewRealm(), args[0].Bool())
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
	var evalDepth int
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
		evalDepth++
		defer func() { evalDepth-- }()
		return s.Evaluate(h.ctx)
	})
	index := len(h.entries)
	detach := r.FuncOf("detachGlobal", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		// d8 keeps the main realm and every realm on the Realm.eval stack attached.
		if index == 0 || evalDepth != 0 {
			return false, nil
		}
		h.host.DetachGlobalProxy(r)
		return true, nil
	})
	h.entries = append(h.entries, r.ValueOf(map[string]any{
		"global":       h.host.InstallGlobalProxy(r, view, shareAccess),
		"evaluate":     evaluate,
		"detachGlobal": detach,
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
		if (!Realm.eval(r, 'console.log instanceof Function && printErr instanceof Function')) {
			throw Error("shell helpers installed in the wrong realm");
		}
		const buffer = Realm.eval(r, 'readbuffer("./bytes.bin")');
		if (!(buffer instanceof Realm.eval(r, "ArrayBuffer"))) throw Error("wrong buffer realm");
		if (new Uint8Array(buffer.transfer())[0] !== 0xff) throw Error("file byte changed");
		const childError = Realm.eval(r, 'try { readbuffer("missing"); } catch (e) { e.constructor === Error; }');
		if (childError !== true) throw Error("expected a file error from the child realm");
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

		const createShared = Realm.eval(r, "Realm.createAllowCrossRealmAccess");
		Realm.global(createShared()).marker = 1;
		const marker = Realm.eval(r, "globalThis.marker = {}");
		function checkPeerOfR(i) {
			if (Realm.eval(i, "Realm.global(" + r + ").marker") !== marker) {
				throw Error("child cannot read its group's global");
			}
			try {
				Realm.global(i).marker;
			} catch (e) {
				if (e instanceof TypeError) return;
				throw e;
			}
			throw Error("child is accessible outside its group");
		}
		const peer = Realm.eval(r, "Realm.createAllowCrossRealmAccess()");
		checkPeerOfR(Realm.eval(peer, "Realm.createAllowCrossRealmAccess()"));

		const handler = new Proxy(Realm.eval(r, "Function.prototype"), {
			apply() { return Realm.createAllowCrossRealmAccess(); },
		});
		Promise.resolve().then(handler).then(checkPeerOfR);

		const afterAwait = Realm.eval(r, "async () => { await 0; return Realm.createAllowCrossRealmAccess(); }");
		afterAwait().then(checkPeerOfR);

		const thenable = Realm.eval(r, "({then(resolve) { resolve(Object.getPrototypeOf(resolve) === Function.prototype); }})");
		Promise.resolve(thenable).then(ownResolver => {
			if (!ownResolver) throw Error("resolver belongs to the wrong realm");
		});
	`
	host.checkRealmScript(t, "test/mjsunit/realm.js", source, fstest.MapFS{
		"bytes.bin": {
			Data: []byte{0xff},
		},
		"helper.js": {
			Data: []byte("var loadedIn = globalThis"),
		},
	})
}

// CheckRealmDetachGlobal checks the lifecycle of a detached realm and its global proxy.
func (host *V8) CheckRealmDetachGlobal(t *testing.T) {
	const source = `
		function mustReject(i) {
			try { Realm.detachGlobal(i); } catch (e) {
				if (e instanceof Error) return;
				throw e;
			}
			throw Error("detached an active realm");
		}
		mustReject(0);
		const r = Realm.createAllowCrossRealmAccess();
		const inner = Realm.create();
		const g = Realm.global(r);
		g.checkParent = () => Realm.eval(inner, "(" + mustReject + ")(" + r + ")");
		Realm.eval(r, "checkParent()");

		const enqueue = Realm.eval(r, "f => Promise.resolve().then(() => f())");
		let ran = false;
		const mark = () => { ran = true; };
		enqueue(mark);
		Realm.detachGlobal(r);
		Realm.detachGlobal(r);
		if (g !== Realm.global(r) || g !== Realm.eval(r, "this")) throw Error("global identity changed");
		if (Realm.eval(r, "x = 42; x") !== 42) throw Error("lost the global binding");
		const read = Realm.eval(r, "g => g.x");
		const typeError = Realm.eval(r, "TypeError");
		for (const target of [g, globalThis]) {
			try { read(target); } catch (e) {
				if (e instanceof typeError) continue;
				throw e;
			}
			throw Error("detached realm retained global access");
		}
		enqueue(mark);
		Promise.resolve().then(() => { if (ran) throw Error("ran a detached realm's job"); });
	`
	host.checkRealmScript(t, "test/mjsunit/detach-global.js", source, fstest.MapFS{})

	const waiters = `
		const r = Realm.createAllowCrossRealmAccess();
		const cells = new Int32Array(new SharedArrayBuffer(4));
		const waiting = Atomics.waitAsync(cells, 0, 0).value.then();
		const waitInChild = Realm.eval(r, "a => Atomics.waitAsync(a, 0, 0).value");
		const childWaiting = new Promise(resolve => waitInChild(cells).then(resolve));
		Realm.detachGlobal(r);
		const notify = Realm.eval(r, "cells => Atomics.notify(cells, 0)");
		notify(cells);
		if (await waiting !== "ok" || await childWaiting !== "ok") throw Error("waiter was not notified");
	`
	host.checkRealmScript(t, "test/mjsunit/detach-waiters.mjs", waiters, fstest.MapFS{})
}

func (host *V8) checkRealmScript(t *testing.T, file, source string, checkout fstest.MapFS) {
	t.Helper()
	test := v8Test{
		file: file,
	}
	checkout[file] = &fstest.MapFile{
		Data: []byte(source),
	}
	if result := test.run(t.Context(), host, checkout, testTimeout); result.outcome != suitePassed {
		t.Error(result.reason)
	}
}
