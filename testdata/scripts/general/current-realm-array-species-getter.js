// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "current realm Array species getter"
flags: [noStrict]
---*/

var calls = 0, receiver;
Object.defineProperty(Array, Symbol.species, {get() {
	calls++; receiver = this; return Array;
}, configurable: true});
var result = [1, 2].slice(1);
assert.sameValue(String([calls, receiver === Array, result.join()].join()), "1,true,2");
