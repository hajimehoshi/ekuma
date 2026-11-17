// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "construction from arrays that start with numbers"
flags: [noStrict]
---*/

var v = {valueOf() { return -1; }};
assert.sameValue(String([new Int8Array([1, 300, "7", v, 2.9]), Int16Array.from([1, 70000, "7", v]), Float32Array.from([1, 2], x => x * 2),
	new Uint8Array([1, , 3]), Uint8Array.from({length: 2, 0: 9})].join(" ")), "1,44,7,-1,2 1,4464,7,-1 2,4 1,0,3 9,0");
