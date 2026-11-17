// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "own properties"
flags: [noStrict]
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
var re = /b/g, li = /b/y, frozen = Object.freeze(/b/g);
Object.defineProperty(re, "flags", {value: ""});
li.lastIndex = {valueOf() { log.push("lastIndex"); return 1; }};
var r = ["abab".replace(re, "x"), "abab".replace(li, "x"), log.join()];
try { "abab".replace(frozen, "x"); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
assert.sameValue(String(r.join(" ")), "axab axab lastIndex TypeError");
