// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "subclass"
flags: [noStrict]
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
class R extends RegExp { exec(s) { log.push(this.flags); return super.exec(s); } }
assert.sameValue(String(["abab".replace(new R("b", "g"), "x"), "a,b".split(new R(",")), log.join()].join(" ")), "axax a,b g,g,g,y,y,y");
