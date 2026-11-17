// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prototype modified during split"
flags: [noStrict]
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
var re = /,/;
var r = ["a,b;c".split(re, {valueOf() { re.compile(";"); return 10; }})];
r.push("a,b".split(/,/, {valueOf() { RegExp.prototype.exec = function () { log.push("exec"); return null; }; return 10; }}));
r.push(log.length > 0);
assert.sameValue(String(r.join(" ")), "a,b;c a,b true");
