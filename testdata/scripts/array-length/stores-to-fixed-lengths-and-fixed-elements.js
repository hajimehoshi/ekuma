// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Stores to the length of an array whose length is not writable fail without converting the value, and stores that would delete a non-configurable element stop above it, in sloppy and strict code on every iteration"
flags: [noStrict]
---*/

function setSloppy(a, v) {
	a.length = v;
}

function setStrict(a, v) {
	"use strict";
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
	var frozen = Object.freeze([0, 1, 2]);
	setSloppy(frozen, 1);
	assert.sameValue(frozen.length, 3);
	assert.sameValue(thrown(() => setStrict(frozen, 1)), TypeError);
	assert.sameValue(frozen.length, 3);

	var fixed = [0, 1, 2];
	Object.defineProperty(fixed, "length", { writable: false });
	setSloppy(fixed, 0);
	assert.sameValue(fixed.join(), "0,1,2");
	assert.sameValue(thrown(() => setStrict(fixed, 0)), TypeError);
	assert.sameValue(fixed.join(), "0,1,2");
	var converted = false;
	setSloppy(fixed, {
		valueOf() {
			converted = true;
			return 0;
		},
	});
	assert.sameValue(converted, false);

	var sealed = Object.seal([0, 1, 2]);
	setSloppy(sealed, 1);
	assert.sameValue(sealed.join(), "0,1,2");
	assert.sameValue(thrown(() => setStrict(sealed, 1)), TypeError);
	assert.sameValue(sealed.length, 3);

	var partial = [0, 1, 2, 3];
	Object.defineProperty(partial, 1, { value: 1, configurable: false });
	setSloppy(partial, 0);
	assert.sameValue(partial.length, 2);
	assert.sameValue(partial.join(), "0,1");
	partial.push(2);
	assert.sameValue(thrown(() => setStrict(partial, 0)), TypeError);
	assert.sameValue(partial.length, 2);

	var accessor = [0, 1, 2];
	Object.defineProperty(accessor, 2, {
		get() {
			return "got";
		},
		configurable: true,
	});
	setStrict(accessor, 2);
	assert.sameValue(accessor.length, 2);
	assert.sameValue(2 in accessor, false);

	var far = [0];
	far[1e6] = 1;
	setStrict(far, 1);
	assert.sameValue(far.length, 1);
	assert.sameValue(1e6 in far, false);
}
