// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "assigned Symbol.match"
flags: [noStrict, noRepeat]
noRepeatReason: "it starts with the original RegExp.prototype[Symbol.match]"
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
RegExp.prototype[Symbol.match] = 0;
assert.sameValue(String("abab".replaceAll(/b/, "x")), "axab");
