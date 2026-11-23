// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "for-of with two bindings over entries observes what replaces the iteration of their arrays"
---*/

function pairs(iterable) {
	var out = [];
	for (const [a, b] of iterable) {
		out.push(a + "" + b);
	}
	return out.join();
}

assert.sameValue(pairs(new Map([["xy", 1]]).keys()), "xy", "the keys of a Map");
assert.sameValue(pairs(new Set(["xy"]).values()), "xy", "the values of a Set");
assert.sameValue(pairs(["xy"].values()), "xy", "the values of an array");

var map = new Map([[1, 2]]);
var arrayIteratorProto = Object.getPrototypeOf([][Symbol.iterator]());
var arrayNext = arrayIteratorProto.next, calls = 0;
arrayIteratorProto.next = function () {
	calls++;
	return arrayNext.call(this);
};
pairs(map);
arrayIteratorProto.next = arrayNext;
assert.sameValue(calls, 2, "a replaced next method of array iterators");

var arrayIterator = Array.prototype[Symbol.iterator];
Array.prototype[Symbol.iterator] = function* () { yield "r"; yield "s"; };
var replaced = pairs(map);
Array.prototype[Symbol.iterator] = arrayIterator;
assert.sameValue(replaced, "rs", "a replaced Array.prototype[Symbol.iterator]");

var setEntries = new Set([1]).entries();
calls = 0;
arrayIteratorProto.return = function () {
	calls++;
	return {};
};
pairs(setEntries);
pairs([[1, 2]]);
delete arrayIteratorProto.return;
assert.sameValue(calls, 2, "a return method of array iterators");

var iteratorProto = Object.getPrototypeOf(arrayIteratorProto);
var arrayEntries = [[1, 2]].entries();
calls = 0;
iteratorProto.return = function () {
	calls++;
	return {};
};
pairs(arrayEntries);
delete iteratorProto.return;
assert.sameValue(calls, 1, "a return method of all iterators");

var lookups = [];
var objectProto = Object.getPrototypeOf(iteratorProto);
Object.setPrototypeOf(iteratorProto, new Proxy(objectProto, {
	get: function (t, k, r) {
		lookups.push(String(k));
		return Reflect.get(t, k, r);
	},
}));
pairs(map);
Object.setPrototypeOf(iteratorProto, objectProto);
assert.sameValue(lookups.indexOf("return") >= 0, true, "a proxy among the prototypes of array iterators");

var kv = new Map([["k", "v"]]);
var mapIteratorProto = Object.getPrototypeOf(new Map().entries());
var mapNext = mapIteratorProto.next;
mapIteratorProto.next = function () {
	var r = mapNext.call(this);
	return r.done ? r : {value: [r.value[1], r.value[0]], done: false};
};
replaced = pairs(kv);
mapIteratorProto.next = mapNext;
assert.sameValue(replaced, "vk", "a replaced next method of Map iterators");

Object.defineProperty(Array.prototype, 1, {
	get: function () {
		return "g";
	},
	configurable: true,
});
replaced = pairs([[1, , 3]]);
delete Array.prototype[1];
assert.sameValue(replaced, "1g", "a pair with a hole");

var own = [1, 2];
own[Symbol.iterator] = function* () { yield "o"; yield "p"; };
assert.sameValue(pairs([own]) + ";" + pairs([[1]]), "op;1undefined", "pairs with their own iterator or one element");

var getters = 0;
var holder = [0, 0];
Object.defineProperty(holder, 1, {
	get: function () {
		getters++;
		return 2;
	},
});
assert.sameValue(pairs(holder.entries()) + ":" + getters, "00,12:1", "array entries with a getter");

var lengths = 0;
var arrayLike = {
	0: "a",
	get length() {
		lengths++;
		return 1;
	},
};
assert.sameValue(pairs(Array.prototype.entries.call(arrayLike)) + ":" + lengths, "0a:2", "the entries of an array-like object");

// A length getter can change how the entry it is about to yield is destructured.
var marker = {}, closed = false;
var changing = Array.prototype.entries.call({
	0: "element",
	get length() {
		Array.prototype[Symbol.iterator] = function* () {
			yield this[1];
			throw marker;
		};
		return 1;
	},
});
changing.return = function () { closed = true; return {}; };
function bindChangingEntry() {
	var first, second, caught;
	try {
		for ([first, second] of changing) {}
	} catch (e) {
		caught = e;
	} finally {
		Array.prototype[Symbol.iterator] = arrayIterator;
	}
	assert.sameValue(first, "element");
	assert.sameValue(caught, marker);
	assert.sameValue(closed, true);
}
bindChangingEntry();

var foreign = $262.createRealm().global;
var foreignMap = new foreign.Map([[1, 2]]);
var foreignArrayIteratorProto = Object.getPrototypeOf(new foreign.Array()[foreign.Symbol.iterator]());
var foreignNext = foreignArrayIteratorProto.next;
calls = 0;
foreignArrayIteratorProto.next = function () {
	calls++;
	return foreignNext.call(this);
};
pairs(foreignMap);
pairs([new foreign.Array(1, 2)]);
assert.sameValue(calls, 4, "a replaced next method of the array iterators of another realm");
