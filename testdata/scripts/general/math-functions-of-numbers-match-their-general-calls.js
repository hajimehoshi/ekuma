// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Math functions of numbers match their general calls"
---*/

// The Math functions called with numbers give what they give for the same numbers in Number
// objects, which they convert. Only calls with as many arguments as the length of the function
// take the numbers as they are.
var numbers = [0, -0, 0.5, -0.5, 1.5, -2.5, 2.5, 1, -1, 3, 1e-310, 2 ** 31, -(2 ** 31) - 1, 2 ** 53 + 2, 1e300, Infinity, -Infinity, NaN];
var unary = ["abs", "acos", "acosh", "asin", "asinh", "atan", "atanh", "cbrt", "ceil", "clz32", "cos", "cosh", "exp", "expm1", "floor", "fround", "f16round", "log", "log1p", "log10", "log2", "round", "sign", "sin", "sinh", "sqrt", "tan", "tanh", "trunc"];
var binary = ["atan2", "pow", "imul", "max", "min"];
var wrong = [];
for (var k = 0; k < 2; k++) {
	for (var name of unary) {
		for (var x of numbers) {
			if (!Object.is(Math[name](x), Math[name](Object(x)))) wrong.push(name + "(" + x + ")");
		}
	}
	for (var name of binary) {
		for (var x of numbers) {
			for (var y of numbers) {
				if (!Object.is(Math[name](x, y), Math[name](Object(x), y))) wrong.push(name + "(" + x + ", " + y + ")");
			}
		}
	}
}
assert.sameValue(wrong.join(" "), "");

var r = [];
for (var k = 0; k < 2; k++) {
	var notConstructor;
	try { new Math.floor(1); notConstructor = "no error"; } catch (e) { notConstructor = e.constructor.name; }
	r.push(Math.min(1, 2, 0), Math.max(1, 2, 3), Math.floor(1.5, 9), Math.pow(2), Object.is(Math.min(0, -0), -0), Object.is(Math.max(-0, 0), 0), Math.imul(2 ** 31, 3), notConstructor);
}
assert.sameValue(r.join(), "0,3,1,NaN,true,true,-2147483648,TypeError,0,3,1,NaN,true,true,-2147483648,TypeError");
