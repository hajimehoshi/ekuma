// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "large maxByteLength"
flags: [noStrict]
---*/

// A maxByteLength above 2^31 either keeps the buffer resizable or, where an int
// cannot hold it, is a RangeError.
var r = [];
for (var f of [() => new ArrayBuffer(0, {maxByteLength: 2 ** 32 - 1}), () => new SharedArrayBuffer(0, {maxByteLength: 2 ** 32 - 1})]) {
	try {
		var b = f();
		r.push((b.resizable ?? b.growable) && b.maxByteLength === 2 ** 32 - 1 ? "ok" : "fixed");
	} catch (e) {
		r.push(e instanceof RangeError ? "ok" : e.constructor.name);
	}
}
assert.sameValue(String(r.join()), "ok,ok");
