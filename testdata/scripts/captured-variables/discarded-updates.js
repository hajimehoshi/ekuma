// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Discarded updates of captured bindings check initialization and store after coercion"
---*/

(function () {
	function increment() { value++; }
	function decrement() { --value; }
	let tdzError;
	try {
		increment();
	} catch (error) {
		tdzError = error;
	}
	assert.sameValue(tdzError instanceof ReferenceError, true);
	let value = {
		valueOf() {
			value = 7n;
			decrement();
			assert.sameValue(value, 6n);
			return 40;
		}
	};
	increment();
	assert.sameValue(value, 41);
	decrement();
	assert.sameValue(value, 40);
})();
