// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Set methods call a has method of a set other than the built-in one"
flags: [noStrict]
---*/

var other = new Set([1, 2, 3]), calls = 0;
other.has = function (x) { calls++; return x === 2; };
var a = new Set([1, 2]).intersection(other), b = new Set([1, 2, 3, 4, 5, 6]).difference(new Set([2, 4]));
assert.sameValue(String([...a].join() + "|" + calls + "|" + [...b].join()), "2|2|1,3,5,6");
