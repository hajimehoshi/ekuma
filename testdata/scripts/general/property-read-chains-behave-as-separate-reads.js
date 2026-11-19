// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "property read chains behave as separate reads"
---*/

// Every reader runs several rounds, so that its reads run through their caches too. Each
// reader has caches of its own.
var rounds = 3;

function readOwnAndInherited(o) {
	return o.a.b;
}
var ownB = {a: {b: "own"}};
var inheritedB = {a: Object.create({b: "inherited"})};
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readOwnAndInherited(ownB), "own", "own properties");
	assert.sameValue(readOwnAndInherited(inheritedB), "inherited", "an inherited second link");
}

function readManyShapes(o) {
	return o.a.b;
}
var manyShapes = [
	{a: {b: 0}},
	{x: 0, a: {x: 0, b: 1}},
	{y: 0, a: {y: 0, b: 2}},
	{z: 0, a: {z: 0, b: 3}},
];
for (var i = 0; i < rounds; i++) {
	for (var j = 0; j < manyShapes.length; j++) {
		assert.sameValue(readManyShapes(manyShapes[j]), j, "more shapes than a cache holds");
	}
}

function readGetterFirst(o) {
	return o.a.b;
}
var ownGetterFirst = {
	get a() {
		return {b: "own getter"};
	},
};
var inheritedGetterFirst = Object.create({
	get a() {
		return {b: "inherited getter"};
	},
});
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readGetterFirst(ownGetterFirst), "own getter", "an own getter on the first link");
	assert.sameValue(readGetterFirst(inheritedGetterFirst), "inherited getter", "an inherited getter on the first link");
}

function readGetterSecond(o) {
	return o.a.b;
}
var ownGetterSecond = {
	a: {
		get b() {
			return this.v;
		},
		v: "own getter",
	},
};
var inheritedGetterSecond = {
	a: Object.create({
		get b() {
			return this.v;
		},
	}),
};
inheritedGetterSecond.a.v = "inherited getter";
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readGetterSecond(ownGetterSecond), "own getter", "an own getter on the second link");
	assert.sameValue(readGetterSecond(inheritedGetterSecond), "inherited getter", "an inherited getter on the second link");
}

function readAbsent(o) {
	return o.a.b;
}
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readAbsent({a: {}}), undefined, "an absent second link");
}

function readPrimitiveSecond(o) {
	return o.a.constructor;
}
function readPrimitiveFirst(v) {
	return v.constructor.name;
}
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readPrimitiveSecond({a: "s"}), String, "a string before the second link");
	assert.sameValue(readPrimitiveSecond({a: 1}), Number, "a number before the second link");
	assert.sameValue(readPrimitiveFirst("s"), "String", "a string before the first link");
	assert.sameValue(readPrimitiveFirst(1), "Number", "a number before the first link");
}

// A property added to a prototype between an object and the holder of a cached property
// shadows that property.
function A() {
}
A.prototype.m = function () {
	return "A";
};
function B() {
}
B.prototype = Object.create(A.prototype);
var methodHolder = {p: new B()};
function callMethod(o) {
	return o.p.m();
}
for (var i = 0; i < rounds; i++) {
	assert.sameValue(callMethod(methodHolder), "A", "a prototype method as the second link");
}
B.prototype.m = function () {
	return "B";
};
for (var i = 0; i < rounds; i++) {
	assert.sameValue(callMethod(methodHolder), "B", "a shadowed prototype method as the second link");
}

var upper = {p: {q: "upper"}};
var middle = Object.create(upper);
var lower = Object.create(middle);
function readInheritedFirst(o) {
	return o.p.q;
}
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readInheritedFirst(lower), "upper", "an inherited first link");
}
middle.p = {q: "middle"};
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readInheritedFirst(lower), "middle", "a shadowed inherited first link");
}

function readThree(o) {
	return o.a.b.c;
}
function readFour(o) {
	return o.a.b.c.d;
}
var deep = {a: {b: {c: {d: "d"}}}};
var getterInDeep = {
	a: {
		get b() {
			return {c: {d: "after a getter"}};
		},
	},
};
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readThree(deep), deep.a.b.c, "three links");
	assert.sameValue(readFour(deep), "d", "four links");
	assert.sameValue(readFour(getterInDeep), "after a getter", "four links with a getter on the second");
}

function readAfterJump(x, o) {
	return (x || o.a).b;
}
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readAfterJump(null, {a: {b: "read"}}), "read", "the first link");
	assert.sameValue(readAfterJump({b: "jumped"}, {a: {b: "read"}}), "jumped", "a jump past the first link");
}

// A second link that throws leaves its destination as it was.
function readInto(o) {
	var r = "unchanged";
	try {
		r = o.a.b;
	} catch (e) {
		return r;
	}
	return r;
}
var throwingGetter = {
	a: {
		get b() {
			throw new Test262Error();
		},
	},
};
for (var i = 0; i < rounds; i++) {
	assert.sameValue(readInto({a: {b: "read"}}), "read", "a second link that reads");
	assert.sameValue(readInto(throwingGetter), "unchanged", "a getter that throws on the second link");
	assert.sameValue(readInto({a: null}), "unchanged", "null before the second link");
	assert.sameValue(readInto({a: undefined}), "unchanged", "undefined before the second link");
}
