// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "exec changed during replace"
flags: [noStrict]
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
var re = /b/g, n = 0;
Object.defineProperty(re, "exec", {get() {
	if (++n < 3) return RegExp.prototype.exec;
	return function (s) { log.push("exec"); return n === 3 ? {0: "c", index: 5, length: 1} : null; };
}});
assert.sameValue(String(["abcabc".replace(re, "[$&]"), log.join()].join(" ")), "a[b]ca[b][c] exec,exec");
