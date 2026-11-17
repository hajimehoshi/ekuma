// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "toLowerCase of a long string keeps its characters whole"
flags: [noStrict]
---*/

var s = "\u00e9" + "a".repeat(65532) + "\u20ac" + "b".repeat(10);
assert.sameValue(String(s.toLowerCase() === s), "true");
