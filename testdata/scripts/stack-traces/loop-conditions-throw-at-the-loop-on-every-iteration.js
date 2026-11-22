// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a loop condition that throws shows the location of the loop on every iteration"
flags: [noStrict]
---*/

// where returns the line and column of the function that f throws in.
function where(f) {
	try {
		f();
	} catch (e) {
		return /:(\d+:\d+)\)?$/.exec(e.stack.split("\n")[1])[1];
	}
}

// A short condition is tested before the body and after it.
assert.sameValue(where(function () {
	let x = Symbol();
	while (x < 1) {
		x = 0;
	}
}), "21:2");
assert.sameValue(where(function () {
	let x = 0;
	while (x < 1) {
		x = Symbol();
	}
}), "27:2");
assert.sameValue(where(function () {
	for (let x = 0, limit = 1; x < limit; x++) {
		limit = Symbol();
	}
}), "32:2");

// A long condition is tested after the body only, which the loop jumps to first.
assert.sameValue(where(function () {
	let a = 1, x = Symbol();
	while (x < a + a + a + a + a + a + a + a) {
		x = 0;
	}
}), "40:2");
