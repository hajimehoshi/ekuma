// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the target and the apply trap of a proxy have the function that called the proxy as their caller"
flags: [noStrict]
---*/

var caller;
function f() { caller = f.caller; }
function callProxy() { new Proxy(f, {})(); }
callProxy();
assert.sameValue(caller, callProxy, "target");

var handler = { apply: function trap(t, thisArg, args) { caller = trap.caller; } };
function callTrap() { new Proxy(f, handler)(); }
callTrap();
assert.sameValue(caller, callTrap, "trap");
