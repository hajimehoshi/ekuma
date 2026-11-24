// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "species getter of RegExp"
flags: [noStrict, noRepeat]
noRepeatReason: "it starts with the original RegExp[Symbol.species]"
---*/

// Warm the built-ins before modifying the RegExp objects.
var log = [];
var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
Object.defineProperty(RegExp, Symbol.species, {get() { log.push("species"); return RegExp; }});
assert.sameValue(String(["a,b".split(/,/), [..."abab".matchAll(/b/g)].length, log.join()].join(" ")), "a,b 2 species,species");
