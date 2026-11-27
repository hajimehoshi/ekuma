// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Stores to the length of a plain array convert values that are not numbers twice, throw a RangeError for numbers that are not array lengths, and grow the array for greater lengths, on every iteration"
---*/

function setLength(a, v) {
	a.length = v;
}

function thrown(f) {
	try {
		f();
	} catch (e) {
		return e.constructor;
	}
}

for (var round = 0; round < 3; round++) {
	var a = [0, 1, 2, 3];
	var log = [];
	setLength(a, {
		valueOf() {
			log.push("valueOf");
			return 2;
		},
	});
	assert.sameValue(log.join(), "valueOf,valueOf");
	assert.sameValue(a.join(), "0,1");
	setLength(a, "1");
	assert.sameValue(a.join(), "0");
	setLength(a, true);
	assert.sameValue(a.length, 1);
	setLength(a, null);
	assert.sameValue(a.length, 0);

	var b = [0, 1, 2, 3];
	for (var v of [1.5, NaN, -1, 2 ** 32, Infinity, undefined, "x"]) {
		assert.sameValue(thrown(() => setLength(b, v)), RangeError);
		assert.sameValue(b.length, 4);
	}
	setLength(b, 6);
	assert.sameValue(b.length, 6);
	assert.sameValue(5 in b, false);
	assert.sameValue(b.join(), "0,1,2,3,,");

	// The conversion runs before the store and can change the array.
	var c = [0, 1, 2, 3];
	setLength(c, {
		valueOf() {
			c.length = 1;
			return 3;
		},
	});
	assert.sameValue(c.length, 3);
	assert.sameValue(c[0], 0);
	assert.sameValue(1 in c, false);
}
