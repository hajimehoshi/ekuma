// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "allocation limit"
flags: [noStrict]
---*/

// A data block that cannot be created is a RangeError instead of an allocation failure.
var r = [];
for (var f of [() => new ArrayBuffer(2 ** 33), () => new Float64Array(2 ** 31), () => new ArrayBuffer(0, {maxByteLength: 2 ** 33})]) {
	try { f(); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
}
assert.sameValue(String(r.join()), "RangeError,RangeError,RangeError");
