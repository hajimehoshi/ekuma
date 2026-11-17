// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "try block"
flags: [noStrict]
---*/

var caught;
try {
	function f(n) {
		"use strict";
		try { return n === 0 ? 0 : f(n - 1); } catch (e) { throw e; }
	}
	f(1e6);
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof RangeError, true);
