// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the captures and the last match of a RegExp on a short string are its slices"
flags: [noStrict]
---*/

const s = String(12345678);
const m = /^12(3)(45)678$/.exec(s);
assert.sameValue(m[1], "3");
assert.sameValue(m[2], "45");
assert.sameValue(RegExp.lastMatch, s);
