// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "current realm Array species is not a constructor"
flags: [noStrict]
---*/

Object.defineProperty(Array, Symbol.species, {value: {}});
var caught;
try { [1].map(x => x); } catch (e) { caught = e; }
assert.sameValue(String(caught instanceof TypeError), "true");
