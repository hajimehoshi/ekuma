// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reads that found a property on a prototype see its value after the prototype grew"
---*/

// grow gives o more properties than it has room for, first with a shared shape, then past the
// number at which an object gets a shape of its own.
function grow(o, prefix) {
	for (var i = 0; i < 300; i++) {
		o[prefix + i] = i;
	}
}

function Inner() {}
Inner.prototype.y = "y1";
function Outer() {}
Outer.prototype.x = "x1";
Outer.prototype.m = function () { return "m1"; };
Outer.prototype.inner = new Inner();

var o = new Outer();
function readX(v) { return v.x; }
function callM(v) { return v.m(); }
function readChain(v) { return v.inner.y; }
function check(x, m, y) {
	for (var i = 0; i < 2; i++) {
		assert.sameValue(readX(o), x);
		assert.sameValue(callM(o), m);
		assert.sameValue(readChain(o), y);
	}
}
check("x1", "m1", "y1");
grow(Outer.prototype, "p");
grow(Inner.prototype, "q");
Outer.prototype.x = "x2";
Outer.prototype.m = function () { return "m2"; };
Inner.prototype.y = "y2";
check("x2", "m2", "y2");

// JSON.stringify looks toJSON up for each value at one depth with one cache.
var replaced = false;
Outer.prototype.toJSON = function () {
	if (!replaced) {
		replaced = true;
		grow(Outer.prototype, "r");
		Outer.prototype.toJSON = function () { return "j2"; };
	}
	return "j1";
};
assert.sameValue(JSON.stringify([o, o]), '["j1","j2"]');

// A method call on a RegExp literal reads the method with a cache of its own.
function testLiteral(s) { return /a/.test(s); }
assert.sameValue(testLiteral("a"), true);
assert.sameValue(testLiteral("a"), true);
var test = RegExp.prototype.test;
grow(RegExp.prototype, "s");
RegExp.prototype.test = function () { return "replaced"; };
assert.sameValue(testLiteral("a"), "replaced");
RegExp.prototype.test = test;
