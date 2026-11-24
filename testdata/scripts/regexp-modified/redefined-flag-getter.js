// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "redefined flag getter"
flags: [noStrict, noRepeat]
noRepeatReason: "it starts with the original RegExp.prototype.global"
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
Object.defineProperty(RegExp.prototype, "global", {get() { log.push("global"); return false; }});
assert.sameValue(String([/b/g.flags, "abab".replace(/b/g, "x"), log.length].join(" ")), " axab 2");
