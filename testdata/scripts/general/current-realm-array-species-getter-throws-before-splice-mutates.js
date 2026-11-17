// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "current realm Array species getter throws before splice mutates"
flags: [noStrict]
---*/

var sentinel = {}, caught, a = [1, 2];
Object.defineProperty(Array, Symbol.species, {get() { throw sentinel; }});
try { a.splice(0, 1); } catch (e) { caught = e; }
assert.sameValue(String([caught === sentinel, a.join()].join("|")), "true|1,2");
