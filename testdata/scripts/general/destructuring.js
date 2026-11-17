// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "destructuring"
flags: [noStrict]
---*/

var {a, b: [c, d = 4, ...rest], ...others} = {a: 1, b: [2, undefined, 5, 6], e: 7, f: 8};
assert.sameValue(String([a, c, d, rest, Object.keys(others)].join("|")), "1|2|4|5,6|e,f");
