// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "concat into a species array keeps its length until the end"
flags: [noStrict]
---*/

var created, seen;
class C extends Array { static get [Symbol.species]() { return function() { return created = new Array(5); }; } }
var c = new C(); c.push(1, 2);
var r = c.concat({ get [Symbol.isConcatSpreadable]() { seen = created.length; return false; } });
assert.sameValue(String([seen, r.length].join()), "5,3");
