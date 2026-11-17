// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "species constructor"
flags: [noStrict]
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
function C(p, f) { log.push("C " + f); return new RegExp(p, f); }
C[Symbol.species] = C;
RegExp.prototype.constructor = C;
var r = ["a,b".split(/,/), [..."abab".matchAll(/b/g)].length];
delete RegExp.prototype.constructor;
Object.defineProperty(RegExp, Symbol.species, {get() { log.push("species"); return C; }});
r.push(Object.getPrototypeOf(/b/g).constructor === Object, log.join());
assert.sameValue(String(r.join(" ")), "a,b 2 true C y,C g");
