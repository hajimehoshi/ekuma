// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a chain of callable proxies"
flags: [noStrict]
---*/

var caught;
try {
	var f = function() {};
	for (var i = 0; i < 100000; i++) f = new Proxy(f, {});
	f();
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof RangeError, true);
