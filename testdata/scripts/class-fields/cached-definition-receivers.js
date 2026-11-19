// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a field defines its property on every receiver as its kind of object allows"
flags: [onlyStrict]
---*/

var receiver;
class Base {
	constructor() {
		return receiver;
	}
}
class C extends Base {
	a = 1;
	b;
}

// Fill the definitions on ordinary receivers first.
receiver = {};
new C();
receiver = {};
var o = new C();
assert.sameValue(Object.keys(o).join(), "a,b");
assert.sameValue(o.b, undefined);

receiver = Object.preventExtensions({});
var threw = false;
try {
	new C();
} catch (e) {
	threw = e instanceof TypeError;
}
assert.sameValue(threw, true);
assert.sameValue(Object.keys(receiver).length, 0);

var log = [];
receiver = new Proxy({}, {
	defineProperty(t, k, d) {
		log.push(k);
		return Reflect.defineProperty(t, k, d);
	}
});
new C();
assert.sameValue(log.join(), "a,b");

receiver = [];
var a = new C();
assert.sameValue(a.a, 1);
assert.sameValue(Object.keys(a).join(), "a,b");

receiver = {a: 0};
var p = new C();
assert.sameValue(p.a, 1);
assert.sameValue(Object.keys(p).join(), "a,b");

receiver = {};
var child = Object.create(receiver);
function getA(o) {
	return o.a;
}
assert.sameValue(getA(child), undefined);
assert.sameValue(getA(child), undefined);
new C();
assert.sameValue(getA(child), 1);
