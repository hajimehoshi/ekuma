// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Math.log10 of powers of ten"
flags: [noStrict]
---*/

var bad = [];
for (var e = -307; e <= 308; e++) {
	if (Math.log10(Number("1e" + e)) !== e) bad.push(e);
}
assert.sameValue(String(bad.join()), "");
