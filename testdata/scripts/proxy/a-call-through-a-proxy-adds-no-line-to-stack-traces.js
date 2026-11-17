// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a call through a proxy adds no line to stack traces"
flags: [noStrict]
---*/

function f() { throw new Error(); }
function stack(g) { try { g(); } catch (e) { return e.stack.split("\n").length; } }
var p = new Proxy(f, {});
assert.sameValue(String(stack(function() { p(); }) === stack(function() { f(); })), "true");
