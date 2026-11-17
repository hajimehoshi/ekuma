// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "more arguments than the frame of the caller holds"
flags: [onlyStrict]
---*/

function small(n, acc) {
	return n === 0 ? acc : big(n, acc, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20);
}
function big(n, acc, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15, a16, a17, a18, a19, a20) {
	return small(n - 1, acc + a1 + a20);
}
assert.sameValue(String(small(1e5, 0)), "2100000");
