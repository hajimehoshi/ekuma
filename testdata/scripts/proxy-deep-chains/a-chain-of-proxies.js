// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a chain of proxies"
flags: [noStrict]
---*/

var caught;
try {
	var p = {};
	for (var i = 0; i < 100000; i++) p = new Proxy(p, {});
	p.x;
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof RangeError, true);
