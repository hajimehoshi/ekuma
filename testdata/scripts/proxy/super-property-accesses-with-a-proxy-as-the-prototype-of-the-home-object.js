// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "super property accesses with a proxy as the prototype of the home object"
flags: [noStrict]
---*/

var log = [];
var proto = new Proxy({}, {
	get(t, k, r) { log.push("get:" + k + ":" + (r === obj)); return 1; },
	set(t, k, v, r) { log.push("set:" + k + ":" + (r === obj)); return true; },
});
var obj = { __proto__: proto, m() { super.y = 2; return super.x; } };
assert.sameValue(String(obj.m() + " " + log.join()), "1 set:y:true,get:x:true");
