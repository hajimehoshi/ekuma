// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "current realm Array species value"
flags: [noStrict]
---*/

class Other extends Array {}
Object.defineProperty(Array, Symbol.species, {value: Other, configurable: true});
var result = [1].map(x => x + 1);
assert.sameValue(String([result instanceof Other, result.length, result[0]].join()), "true,1,2");
