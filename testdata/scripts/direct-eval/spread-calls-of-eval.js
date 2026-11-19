// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "spread calls of eval and of another function named eval, in and out of tail position"
flags: [noStrict]
---*/

var x = "global";
function direct() {
	var x = "local";
	return eval(...["x"]);
}
function directInTailPosition() {
	"use strict";
	var x = "strict local";
	return eval(...["x"]);
}
function other() {
	var eval = function (...args) {
		return args.join();
	};
	var inTailPosition = function () {
		"use strict";
		return eval(...[3, 4]);
	};
	return [eval(...[1, 2]), inTailPosition()].join(";");
}
assert.sameValue(direct(), "local");
assert.sameValue(directInTailPosition(), "strict local");
assert.sameValue(other(), "1,2;3,4");
