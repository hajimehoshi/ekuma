// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "fill through a setter on the prototype"
flags: [noStrict]
---*/

var log = [];
Object.defineProperty(Array.prototype, 1, {set(v) { log.push(v); }, configurable: true});
var a = new Array(3).fill(7);
assert.sameValue(String([a.hasOwnProperty(0), a.hasOwnProperty(1), a.hasOwnProperty(2), log.join()].join()), "true,false,true,7");
