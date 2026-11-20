// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "typed array set combines the target subarray's byte offset with the element offset"
flags: [noStrict]
---*/

const values = new Uint16Array(new SharedArrayBuffer(14));
values.set([11, 22, 33, 44, 55, 66, 77]);
values.subarray(1).set(values.subarray(2, 5), 2);
assert.sameValue(values.join(), "11,22,33,33,44,55,77");
