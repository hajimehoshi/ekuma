// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "for-of disposes the binding before it closes the iterator"
flags: [noStrict]
---*/

var log = [];
function res(name, error) {
	return { [Symbol.dispose]() { log.push(name); if (error) throw error; } };
}
var iterable = {
	[Symbol.iterator]() {
		return {
			next() { return { value: res("v"), done: false }; },
			return() { log.push("close"); return {}; },
		};
	},
};
for (using v of iterable) break;
try { for (using v of iterable) throw "body"; } catch (e) { log.push(e); }
assert.sameValue(String(log.join()), "v,close,v,close,body");
