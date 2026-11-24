// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "assigned exec"
flags: [noStrict, noRepeat]
noRepeatReason: "it starts with the original RegExp.prototype.exec"
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
RegExp.prototype.exec = function (s) { log.push("exec"); return null; };
assert.sameValue(String(["abab".replace(/b/g, "x"), /b/.test("abab"), "abab".match(/b/g), "a,b".split(/,/), log.length].join(" ")), "abab false  a,b 6");
