// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "getter of caller"
flags: [noStrict]
---*/

function f() {}
function g() {}
var get = Object.getOwnPropertyDescriptor(f, "caller").get;
var r = [typeof get, get === Object.getOwnPropertyDescriptor(g, "caller").get];
try { get.call(function() { "use strict"; }); } catch (e) { r.push(e.constructor.name); }
assert.sameValue(String(r.join()), "function,true,TypeError");
