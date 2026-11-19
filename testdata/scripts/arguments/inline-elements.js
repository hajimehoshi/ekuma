// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arguments objects keep their elements when they grow past the arguments of the call"
---*/

function grow() {
	Array.prototype.push.call(arguments, "x", "y");
	arguments[0] = "z";
	return Array.prototype.join.call(arguments);
}
assert.sameValue(grow(), "z,y");
assert.sameValue(grow(1), "z,x,y");
assert.sameValue(grow(1, 2, 3, 4), "z,2,3,4,x,y");
assert.sameValue(grow(1, 2, 3, 4, 5), "z,2,3,4,5,x,y");

function keep(a, b) {
	"use strict";
	var args = arguments;
	return function () {
		return Array.prototype.join.call(args);
	};
}
var k1 = keep(1, 2, 3), k2 = keep(4);
assert.sameValue(k1() + ";" + k2(), "1,2,3;4");
