// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "lengths of objects read at a warm site"
---*/

// The length read at one site of arguments objects, of objects with an own length in
// different slots, of a function, and of an object that inherits it, whose prototype then
// changes the value and makes it a getter.
function len(x) {
	return x.length;
}
function args() {
	return len(arguments);
}
function strictArgs() {
	"use strict";
	return len(arguments);
}
function Proto() {}
Proto.prototype.length = 5;
var inherits = new Proto();
var r = [];
for (var k = 0; k < 3; k++) {
	r.push([args(), args(1, 2), strictArgs(1, 2, 3), len({length: 4}), len({a: 1, length: 6}), len(function (a, b) {}), len(inherits)].join());
}
Proto.prototype.length = 7;
r.push(len(inherits));
Object.defineProperty(Proto.prototype, "length", {get() { return 8; }});
r.push(len(inherits));
assert.sameValue(r.join(" "), "0,2,3,4,6,2,5 0,2,3,4,6,2,5 0,2,3,4,6,2,5 7 8");

assert.sameValue(len(Object.create(String.prototype)), 0);
assert.sameValue(len("é"), 1);
