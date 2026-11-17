// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "private elements consult a proxy's isExtensible trap"
flags: [noStrict]
---*/

var log = [];
function trap(name) { return { isExtensible(t) { log.push(name); return Reflect.isExtensible(t); } }; }
class C extends function (o) { return o; } {
	#a() {} get #b() { return 1; } set #b(v) {} #f = log.push("f"); #g = 1;
	static has(o) { return #a in o && #b in o && #f in o && #g in o; } }
var p = new Proxy({}, trap("x"));
new C(p);
var r = log.join() + " " + C.has(p);
log = [];
var q = new Proxy(Object.preventExtensions({}), trap("y"));
try { new C(q); } catch (e) { r += " " + e.name; }
assert.sameValue(String(r + " " + log.join() + " " + C.has(q)), "x,f,x,x true TypeError y false");
