// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "sloppy code"
flags: [noStrict]
---*/

var caught;
try {
	function f(n) { return n === 0 ? 0 : f(n - 1); } f(1e6);
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof RangeError, true);
