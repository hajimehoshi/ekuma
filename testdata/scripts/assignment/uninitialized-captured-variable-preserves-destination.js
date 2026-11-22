// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a failed read of a captured variable preserves the destination of an assignment"
---*/

(function () {
	function read() {
		let value = 42;
		try {
			value = later;
		} catch (e) {
			assert.sameValue(e instanceof ReferenceError, true);
			return value === 42;
		}
	}
	assert.sameValue(read(), true);
	let later;
})();
