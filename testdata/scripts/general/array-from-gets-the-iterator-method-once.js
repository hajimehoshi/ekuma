// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Array.from gets the iterator method once"
flags: [noStrict]
---*/

var n = 0;
var o = { get [Symbol.iterator]() { n++; return function() { return [1, 2].values(); }; } };
assert.sameValue(String(Array.from(o).join() + "|" + n), "1,2|1");
