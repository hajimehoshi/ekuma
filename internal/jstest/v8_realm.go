// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"context"
	_ "embed"
	"errors"
	"slices"
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
	host    *V8
	vm      *ekuma.VM
	ctx     context.Context
	shell   v8Shell
	loop    *v8EventLoop
	scripts *v8Scripts
	shared  ekuma.Value
	entries []*v8RealmEntry

	// Each Realm.eval protects its caller until the evaluation returns.
	stack []*ekuma.Realm
}

type v8RealmEntry struct {
	realm  *ekuma.Realm
	object ekuma.Value
}

func (h *v8Realms) add(r *ekuma.Realm, shareAccess bool) (int, error) {
	index := len(h.entries)
	entry, err := h.newEntry(index, r, nil, shareAccess)
	if err != nil {
		return 0, err
	}
	h.entries = append(h.entries, entry)
	return index, nil
}

func (h *v8Realms) newEntry(index int, r, previous *ekuma.Realm, shareAccess bool) (*v8RealmEntry, error) {
	s, err := v8RealmScript()
	if err != nil {
		return nil, err
	}
	init, err := r.BindScript(s).Evaluate(h.ctx)
	if err != nil {
		return nil, err
	}
	if err := h.loop.install(r); err != nil {
		return nil, err
	}
	create := r.FuncOf("create", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return h.add(h.vm.NewRealm(), args[0].Bool())
	})
	lookup := r.FuncOf("lookup", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		i := args[0].Int()
		if i < 0 || i >= len(h.entries) || h.entries[i] == nil {
			return ekuma.Undefined, nil
		}
		return h.entries[i].object, nil
	})
	current := r.FuncOf("current", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		for i, entry := range h.entries {
			if entry != nil && h.host.IsEnteredRealm(entry.realm) {
				return i, nil
			}
		}
		return ekuma.Undefined, nil
	})
	view, err := init.Invoke(h.ctx, create, lookup, current, r.FuncOf("owner", h.owner), h.shared)
	if err != nil {
		return nil, err
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
		caller := h.scripts.current
		h.stack = append(h.stack, caller)
		h.scripts.current, h.scripts.next = r, r
		defer func() {
			h.scripts.current, h.scripts.next = caller, caller
			h.stack = slices.Delete(h.stack, len(h.stack)-1, len(h.stack))
		}()
		return h.host.CallScript(s), nil
	})
	change := r.FuncOf("change", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return h.change(index, args[0].String())
	})
	global := h.host.InstallGlobalProxy(r, view, previous, shareAccess)
	if err := h.shell.install(h.ctx, h.host, r, h.scripts); err != nil {
		return nil, err
	}
	return &v8RealmEntry{
		realm: r,
		object: r.ValueOf(map[string]any{
			"global":   global,
			"evaluate": evaluate,
			"change":   change,
		}),
	}, nil
}

func (h *v8Realms) change(index int, operation string) (bool, error) {
	r := h.entries[index].realm
	if operation == "switch" {
		h.scripts.next = r
		return true, nil
	}
	if index == 0 || r == h.scripts.current || r == h.scripts.next || slices.Contains(h.stack, r) {
		return false, nil
	}
	switch operation {
	case "detachGlobal":
		h.host.DetachGlobalProxy(r)
		return true, nil
	case "dispose":
		h.host.DetachGlobalProxy(r)
		h.entries[index] = nil
	case "navigate", "navigateSameOrigin":
		entry, err := h.newEntry(index, h.vm.NewRealm(), r, operation == "navigateSameOrigin")
		if err != nil {
			return false, err
		}
		h.entries[index] = entry
	default:
		panic("unknown realm operation: " + operation)
	}
	delete(h.scripts.contexts, r)
	return true, nil
}

func (h *v8Realms) owner(this ekuma.Value, args []ekuma.Value) (any, error) {
	if len(args) == 0 || (args[0].Type() != ekuma.TypeObject && args[0].Type() != ekuma.TypeFunction) {
		return nil, errors.New("Invalid argument")
	}
	r := h.host.ObjectCreationRealm(args[0])
	for i, entry := range h.entries {
		if entry != nil && entry.realm == r {
			return i, nil
		}
	}
	return ekuma.Undefined, nil
}

// CheckRealm checks script evaluation and global object access across V8 realms.
func (host *V8) CheckRealm(t *testing.T) {
	const source = `
		const r = Realm.create();
		const [entered, current] = Realm.eval(r, "[Realm.current(), Realm.current]");
		if (entered !== r || current() !== 0) throw Error("wrong entered realm");
		// Realm 0 drains the jobs, enqueueRealm creates them, and r owns the callbacks.
		const enqueueRealm = Realm.create();
		const enqueuePromise = Realm.eval(enqueueRealm, "Promise");
		const then = enqueuePromise.prototype.then;
		then.call(Promise.resolve(), current).then(i => {
			if (i !== r) throw Error("reaction entered realm " + i + ", expected " + r);
		});
		const thenable = Realm.eval(r, "({then(resolve) { resolve([Realm.current(), resolve]); }})");
		enqueuePromise.resolve(thenable).then(([entered, resolver]) => {
			if (entered !== r) throw Error("thenable job entered realm " + entered + ", expected " + r);
			if (Object.getPrototypeOf(resolver) !== Realm.eval(r, "Function.prototype")) {
				throw Error("resolver belongs to the wrong realm");
			}
		});

		const rejectionRealms = [];
		const rejected = Promise.reject(42);
		rejected.constructor = {
			[Symbol.species]: class {
				constructor(executor) {
					executor(() => {}, () => rejectionRealms.push(Realm.current()));
				}
			},
		};
		// The absent rejection handler uses the fulfillment handler's realm.
		then.call(rejected, current);
		// With both handlers absent, the borrowed then method enqueues the job.
		then.call(rejected);
		Promise.resolve().then(() => {
			if (rejectionRealms.join() !== [r, enqueueRealm].join()) {
				throw Error("unexpected entered realms for absent handlers: " + rejectionRealms);
			}
		});
		if (Realm.eval(r, 'd8.file.execute("helper.js"); loadedIn') !== Realm.global(r)) {
			throw Error("script loaded in the wrong realm");
		}
		if (!Realm.eval(r, 'console.log instanceof Function && printErr instanceof Function')) {
			throw Error("shell helpers installed in the wrong realm");
		}
		function checkGlobalPrototype() {
			const proto = Object.getPrototypeOf(globalThis);
			const keys = Reflect.ownKeys(proto);
			if (Object.getPrototypeOf(proto) !== Object.prototype || keys.length !== 1 || keys[0] !== "constructor") {
				throw Error("wrong global prototype");
			}
			const ctor = globalThis.constructor;
			const desc = Object.getOwnPropertyDescriptor(proto, "constructor");
			if (Object.hasOwn(globalThis, "constructor") || desc.value !== ctor || !desc.writable ||
				desc.enumerable || !desc.configurable || ctor.prototype !== proto || ctor.name !== "" ||
				Reflect.ownKeys(ctor).join() !== "length,name,prototype") {
				throw Error("wrong global constructor");
			}
			if (ctor() !== globalThis || ctor.call(proto) !== proto || Object.getPrototypeOf(new ctor()) !== proto) {
				throw Error("wrong global constructor receiver");
			}
			const tag = Object.getOwnPropertyDescriptor(globalThis, Symbol.toStringTag);
			if (!tag || tag.value !== "global" || !tag.writable || !tag.enumerable || !tag.configurable) {
				throw Error("wrong global tag descriptor");
			}
		}
		checkGlobalPrototype();
		Realm.eval(r, "(" + checkGlobalPrototype + ")()");
		const Div = Realm.eval(r, "d8.dom.Div");
		if (Div() !== Realm.global(r)) throw Error("wrong global receiver for Div()");
		const unbranded = Object.create(Div.prototype);
		try {
			Div.call(unbranded).nodeType;
			throw Error("calling Div branded its receiver");
		} catch (e) { if (!(e instanceof Realm.eval(r, "TypeError"))) throw e; }
		const mark = Realm.eval(r, 'performance.mark("child")');
		if (performance.measure("elapsed", "child").startTime !== mark.startTime) {
			throw Error("performance mark lost across realms");
		}
		if (typeof version() !== "string") throw Error("expected a version string");
		try {
			version.call(Realm.global(r));
			throw Error("version accepted a foreign global");
		} catch (e) { if (!(e instanceof TypeError)) throw e; }
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

// CheckRealmOwner checks creation realm lookup across realm boundaries.
func (host *V8) CheckRealmOwner(t *testing.T) {
	const source = `
		const child = Realm.create();
		const foreignOwner = Realm.eval(child, "Realm.owner");
		for (const args of [[], [null], [1]]) {
			let error;
			try { foreignOwner(...args); } catch (e) { error = e; }
			if (error?.constructor !== Realm.eval(child, "Error")) throw Error("expected the child's Error");
		}
		const [obj, C] = Realm.eval(child, "[Object.create(null), function C() {}]");
		function check(expected, value) {
			const actual = Realm.owner(value);
			if (actual !== expected) throw Error("expected owner " + expected + ", got " + actual);
		}
		check(child, obj);
		Object.setPrototypeOf(obj, Object.prototype);
		check(child, obj);
		const local = Object.create(obj);
		const peer = Realm.create();
		check(peer, Realm.eval(peer, "Object.create")(obj));
		check(0, local);
		check(child, C);
		check(child, C.prototype);
		check(child, new C());
		check(child, Reflect.construct(C, [], Object));
		check(child, Function.prototype.bind.call(C, null));
		check(child, Realm.eval(child, "async () => {}")());
		check(child, Realm.eval(child, "Intl").NumberFormat);
		const proxy = Proxy.revocable(C, {});
		proxy.revoke();
		check(0, proxy.proxy);
		if (foreignOwner(globalThis, child) !== 0) throw Error("wrong global owner");
		Object.setPrototypeOf(obj, null);
		const global = Realm.global(child);
		Realm.detachGlobal(child);
		check(child, obj);
		Realm.navigate(child);
		check(child, global);
		check(undefined, obj);
		const fresh = Realm.eval(child, "({})");
		Realm.dispose(child);
		check(undefined, fresh);
	`
	host.checkRealmScript(t, "test/mjsunit/realm-owner.js", source, fstest.MapFS{})
}

// CheckRealmShared checks the lifetime and identity of the value shared by a test's realms.
func (host *V8) CheckRealmShared(t *testing.T) {
	const file = "test/mjsunit/realm-shared.js"
	const source = `
		const descriptor = Object.getOwnPropertyDescriptor(Realm, "shared");
		if (!descriptor.writable || !descriptor.enumerable || !descriptor.configurable ||
			!Object.hasOwn(descriptor, "value") || descriptor.value !== undefined) {
			throw Error("wrong initial shared descriptor");
		}
		const token = Symbol();
		Realm.shared = token;
		const child = Realm.create();
		if (Realm.eval(child, "Realm.shared") !== token) throw Error("new realm lost the shared value");
		const object = Realm.eval(child, "Realm.shared = {}");
		if (Realm.shared !== object) throw Error("shared object identity changed");

		const other = Realm.eval(child, "Realm");
		const derived = Object.create(other);
		derived.shared = 1;
		if (derived.shared !== 1 || other.shared !== object) throw Error("inherited write changed the slot");
		Object.defineProperty(other, "shared", {value: token, writable: false, configurable: false});
		if (Realm.shared !== token || Reflect.set(other, "shared", 2)) throw Error("wrong redefinition");
		Realm.shared = object;
		if (Object.getOwnPropertyDescriptor(other, "shared").value !== object) {
			throw Error("read-only property lost the shared value");
		}

		const sibling = Realm.eval(Realm.create(), "Realm");
		delete sibling.shared;
		sibling.shared = 3;
		Object.defineProperty(Realm, "shared", {get() { return 4; }});
		Realm.eval(Realm.create(), "Realm.shared = 5");
		if (other.shared !== 5 || sibling.shared !== 3 || Realm.shared !== 4) {
			throw Error("local replacement changed another realm");
		}
	`
	host.checkRealmScript(t, file, source, fstest.MapFS{})
	host.checkRealmScript(t, file, `
		if (Realm.shared !== undefined) throw Error("shared value leaked from the previous VM");
	`, fstest.MapFS{})
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

// CheckRealmNavigation checks a replacement realm and references retained by its predecessor.
func (host *V8) CheckRealmNavigation(t *testing.T) {
	const source = `
		const peer = Realm.createAllowCrossRealmAccess();
		const r = Realm.createAllowCrossRealmAccess();
		const proxy = Realm.global(r);
		const old = Realm.eval(r, 'var marker = "old"; (' + function () {
			return {
				read: () => globalThis.marker,
				binding: () => marker,
				importModule: () => import("./module.mjs"),
				enqueue: f => Promise.resolve().then(() => f()),
				Error
			};
		} + ')()');
		const mark = () => { throw Error("ran a job in a replaced realm"); };
		old.enqueue(mark);
		if (Realm.navigateSameOrigin(r) !== r) throw Error("changed realm ID");
		if (proxy !== Realm.global(r)) throw Error("replaced the global proxy");
		if (Realm.eval(r, 'typeof marker') !== "undefined") throw Error("retained a global");
		Realm.eval(r, 'var marker = "new"');
		if (old.binding() !== "old" || old.read() !== "new") throw Error("wrong old binding or proxy");
		// Same-origin navigation uses the old realm's default group after detachment.
		try { proxy.marker; throw Error("retained the creator's access"); }
		catch (e) { if (!(e instanceof TypeError)) throw e; }
		old.enqueue(mark);
		old.importModule().then(mark, e => {
			if (e.constructor !== old.Error) throw Error("wrong import rejection");
		});

		const replaced = Realm.eval(r, '() => globalThis.marker');
		const replacedTypeError = Realm.eval(r, 'TypeError');
		const fresh = Realm.eval(r, 'typeof marker', {
			get type() {
				if (Realm.navigate(r) !== r) throw Error("wrong navigation ID");
				return "string";
			}
		});
		if (fresh !== "undefined" || Realm.global(r) !== proxy) throw Error("selected stale realm");
		try { replaced(); throw Error("old function retained cross-origin access"); }
		catch (e) { if (!(e instanceof replacedTypeError)) throw e; }

		try {
			Realm.eval(r, "1", {get type() { Realm.dispose(r); return "string"; }});
			throw "evaluated in disposed realm";
		} catch (e) { if (e.constructor !== Error) throw e; }
		if (Realm.create() <= r) throw Error("reused disposed ID");
		Realm.global(peer).marker = 42;
		if (Realm.eval(peer, "marker") !== 42) throw Error("lost live peer");

		const isolated = Realm.create();
		const sibling = Realm.eval(isolated, "Realm.createAllowCrossRealmAccess()");
		const saved = Realm.global(isolated);
		Realm.detachGlobal(isolated);
		Realm.navigateSameOrigin(isolated);
		Realm.eval(isolated, "var marker = 17");
		const read = Realm.eval(sibling, "g => g.marker");
		if (read(saved) !== 17) throw Error("lost default access group");
	`
	host.checkRealmScript(t, "test/mjsunit/navigation.js", source, fstest.MapFS{
		"module.mjs": {
			Data: []byte("export default 1"),
		},
	})
}

// CheckRealmSwitch checks realm selection across eval, nested loads, and shell inputs.
func (host *V8) CheckRealmSwitch(t *testing.T) {
	const source = `// Files: setup.js selected.mjs
		if (Realm.current() !== 1) throw Error("wrong realm for the next input");
		if (moduleRealm !== globalThis) throw Error("module ran in a different realm");
		Realm.switch(0);
		try { Realm.dispose(1); throw "disposed current"; }
		catch (e) { if (!(e instanceof Error)) throw e; }
		// The first nested script finishes the switch, so the second runs in main.
		load("first.js", "second.js");
		if (!Realm.global(0).finished) throw Error("switch did not apply between loads");
		try { load("missing.js"); throw "loaded missing file"; }
		catch (e) { if (e.constructor !== Realm.global(0).Error) throw e; }
	`
	host.checkRealmScript(t, "test/mjsunit/switch.js", source, fstest.MapFS{
		"setup.js": {
			Data: []byte(`
				const r = Realm.createAllowCrossRealmAccess(), temporary = Realm.create();
				Realm.switch(temporary);
				Realm.eval(r, "Realm.switch(" + temporary + ")");
				Realm.dispose(temporary);
				if (Realm.switch(r) !== undefined || Realm.current() !== 0) throw Error("early switch");
				try { Realm.navigate(r); throw "navigated scheduled realm"; }
				catch (e) { if (!(e instanceof Error)) throw e; }
			`),
		},
		"selected.mjs": {
			Data: []byte(`
				import {g} from "./dep.mjs";
				if (Realm.current() !== 1) throw Error("wrong entered module realm");
				globalThis.moduleRealm = g;
			`),
		},
		"dep.mjs": {
			Data: []byte("export const g = globalThis;"),
		},
		"first.js": {
			Data: []byte(`if (Realm.current() !== 1) throw Error("early switch in load");`),
		},
		"second.js": {
			Data: []byte(`if (Realm.current() !== 0) throw Error("late switch in load"); globalThis.finished = true;`),
		},
	})
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
