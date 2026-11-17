// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "global variables with a proxy on the prototype chain of the global object"
flags: [noStrict]
---*/

var log = [];
var target = { inherited: 1 };
Object.setPrototypeOf(globalThis, new Proxy(target, {
	has(t, k) { if (typeof k === "string") log.push("has:" + k); return Reflect.has(t, k); },
	get(t, k, r) { if (typeof k === "string") log.push("get:" + k); return Reflect.get(t, k, r); },
	set(t, k, v, r) { log.push("set:" + k); return true; },
}));
inherited;
typeof missing;
inherited = 2;
(function() { "use strict"; inherited = 3; })();
assert.sameValue(String(log.join()), "has:inherited,has:inherited,get:inherited,has:missing,has:inherited,has:inherited,set:inherited,has:inherited,has:inherited,set:inherited");
