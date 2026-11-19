// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "calling a huge BigInt throws a TypeError without formatting all of its digits"
flags: [noStrict]
---*/

var caught;
try {
	(1n << (2n ** 30n - 1n))();
} catch (e) {
	caught = e;
}
assert.sameValue(caught instanceof TypeError, true);
