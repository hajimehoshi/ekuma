// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "10 to an integer power"
flags: [noStrict]
---*/

var bad = [];
for (var e = -330; e <= 310; e++) {
	if (Math.pow(10, e) !== Number("1e" + e) || 10 ** e !== Number("1e" + e)) bad.push(e);
}
assert.sameValue(String(bad.join()), "");
