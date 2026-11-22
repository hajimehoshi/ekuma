// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a Map or Set made from another Map or Set calls the iteration methods that replace the original ones"
---*/

function keysOf(c) {
	return [...c.keys()].join();
}

var m = new Map([[1, 1]]);
m[Symbol.iterator] = function* () { yield [2, 2]; };
assert.sameValue(keysOf(new Map(m)), "2", "an own Symbol.iterator of the Map");

var s = new Set([1]);
s[Symbol.iterator] = function* () { yield 2; };
assert.sameValue(keysOf(new Set(s)), "2", "an own Symbol.iterator of the Set");

var mp = Object.create(Map.prototype);
mp[Symbol.iterator] = function* () { yield [3, 3]; };
assert.sameValue(keysOf(new Map(Object.setPrototypeOf(new Map([[1, 1]]), mp))), "3", "the Symbol.iterator of another prototype of the Map");

var sp = Object.create(Set.prototype);
sp[Symbol.iterator] = function* () { yield 3; };
assert.sameValue(keysOf(new Set(Object.setPrototypeOf(new Set([1]), sp))), "3", "the Symbol.iterator of another prototype of the Set");

var mapIterator = Map.prototype[Symbol.iterator];
Map.prototype[Symbol.iterator] = function* () { yield [4, 4]; };
var mc = new Map(new Map([[1, 1]]));
Map.prototype[Symbol.iterator] = mapIterator;
assert.sameValue(keysOf(mc), "4", "a replaced Map.prototype[Symbol.iterator]");

var setIterator = Set.prototype[Symbol.iterator];
Set.prototype[Symbol.iterator] = function* () { yield 4; };
var sc = new Set(new Set([1]));
Set.prototype[Symbol.iterator] = setIterator;
assert.sameValue(keysOf(sc), "4", "a replaced Set.prototype[Symbol.iterator]");

var mapIteratorProto = Object.getPrototypeOf(new Map().entries());
var mapNext = mapIteratorProto.next, mapCalls = 0;
mapIteratorProto.next = function () {
	mapCalls++;
	return mapNext.call(this);
};
mc = new Map(new Map([[1, 1], [2, 2]]));
mapIteratorProto.next = mapNext;
assert.sameValue(mapCalls + ":" + keysOf(mc), "3:1,2", "a replaced next method of Map iterators");

var setIteratorProto = Object.getPrototypeOf(new Set().values());
var setNext = setIteratorProto.next, setCalls = 0;
setIteratorProto.next = function () {
	setCalls++;
	return setNext.call(this);
};
sc = new Set(new Set([1, 2]));
setIteratorProto.next = setNext;
assert.sameValue(setCalls + ":" + keysOf(sc), "3:1,2", "a replaced next method of Set iterators");
