// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the result that yield* passes through is read by the loop"
flags: [noStrict]
---*/

var reads = [];
var inner = {
	[Symbol.iterator]() { return this; },
	next() {
		return {
			get done() { reads.push("done"); return false; },
			get value() { reads.push("value"); return 1; },
		};
	},
};
function* g() { yield* inner; }
for (var x of g()) { reads.push(x); break; }
assert.sameValue(String(reads.join()), "done,done,value,1");
