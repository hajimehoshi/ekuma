// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the functions of a decorator access object read, write and find the element in any object"
flags: [noStrict]
---*/

var accesses = [];
function dec(value, ctx) {
	accesses.push(ctx.access);
}
class C {
	@dec accessor a = 1;
	@dec accessor #b = 2;
}
var c = new C(), [a, b] = accesses;
assert.sameValue([a.get(c), b.get(c)].join(), "1,2");
a.set(c, 3);
b.set(c, 4);
assert.sameValue([a.get(c), b.get(c)].join(), "3,4");
assert.sameValue([a.has(c), b.has(c), a.has({ a: 1 }), b.has({ b: 1 })].join(), "true,true,true,false");
// The functions do not depend on their receiver.
var { get, set, has } = b;
set(c, 5);
assert.sameValue([get(c), has(c), a.get(c)].join(), "5,true,3");
var errors = [get, set, has].map(f => {
	try { f(1); } catch (e) { return e.constructor.name; }
});
assert.sameValue(errors.join(), "TypeError,TypeError,TypeError");
