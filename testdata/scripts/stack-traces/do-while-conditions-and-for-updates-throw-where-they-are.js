// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the condition of a do-while loop and the update of a for loop show their own locations"
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

assert.sameValue(where(function () {
	let x = 0;
	do {
		x = Symbol();
	} while (x < 1);
}), "22:11");
assert.sameValue(where(function () {
	for (let x = 0; x < 2; x++) {
		x = Symbol();
		0;
	}
}), "25:25");
assert.sameValue(where(function () {
	for (let x = 0;
		x < 2;
		x = x * 2) {
		x = Symbol();
	}
}), "33:3");
