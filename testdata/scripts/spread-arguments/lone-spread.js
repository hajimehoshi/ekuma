// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a lone spread argument passes copies of the elements, which the callee cannot change through the spread array"
---*/

var arr = [1, 2, 3];
function mutate(a, b, c) {
	arr[0] = 10;
	arr.length = 1;
	arr.push(20);
	return [a, b, c, arguments.length].join();
}
assert.sameValue(mutate(...arr), "1,2,3,3", "call");
assert.sameValue(arr.join(), "10,20");

arr = [1, 2, 3];
function Ctor(a, b, c) {
	arr.pop();
	this.v = [a, b, c, arguments.length].join();
}
assert.sameValue(new Ctor(...arr).v, "1,2,3,3", "new");

arr = [1, 2, 3];
class Base {
	constructor(a, b, c) {
		arr.shift();
		this.v = [a, b, c].join();
	}
}
class Derived extends Base {
	constructor() {
		super(...arr);
	}
}
assert.sameValue(new Derived().v, "1,2,3", "super");
assert.sameValue(arr.join(), "2,3");

arr = [1, 2, 3];
function tail() {
	"use strict";
	return mutate(...arr);
}
assert.sameValue(tail(), "1,2,3,3", "tail call");

arr = [1, 2, 3];
function rest(...r) {
	r.push(4);
	return r;
}
var r = rest(...arr);
assert.sameValue(r === arr, false, "rest parameter is a new array");
assert.sameValue(arr.join(), "1,2,3");
assert.sameValue(r.join(), "1,2,3,4");

function args() {
	arguments[0] = 9;
	return arguments;
}
arr = [1, 2];
assert.sameValue(args(...arr) === arr, false, "arguments object is a new object");
assert.sameValue(arr.join(), "1,2");

assert.sameValue(mutate(...[1, , 3]), "1,,3,3", "holes are undefined");
arr = [1, 2];
arr[Symbol.iterator] = function* () {
	yield 7;
};
assert.sameValue(mutate(...arr), "7,,,1", "own iterator");

var code = ["arr.length = 0; 5", "ignored"];
arr = code;
assert.sameValue(eval(...arr), 5, "direct eval");
assert.sameValue(arr.length, 0);
