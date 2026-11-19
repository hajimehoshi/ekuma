// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a computed store that fails in strict mode converts its key once"
---*/

var bases = [
	() => Object.freeze({}),
	() => Object.freeze({x: 1}),
	() => ({get x() { return 1; }}),
	() => Object.preventExtensions({}),
	() => "str",
	() => Object.freeze([1, 2]),
];
for (var [i, base] of bases.entries()) {
	var n = 0;
	var k = {toString() { n++; return "x"; }};
	var caught = (function () {
		"use strict";
		try { base()[k] = 1; } catch (e) { return e.constructor; }
	})();
	assert.sameValue(caught, TypeError, "toString key, base " + i);
	assert.sameValue(n, 1, "toString key conversions, base " + i);

	n = 0;
	k = {[Symbol.toPrimitive]() { n++; return "x"; }};
	caught = (function () {
		"use strict";
		try { base()[k] = 1; } catch (e) { return e.constructor; }
	})();
	assert.sameValue(caught, TypeError, "toPrimitive key, base " + i);
	assert.sameValue(n, 1, "toPrimitive key conversions, base " + i);
}
