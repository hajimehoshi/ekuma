// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Closing a for-await iterator reports its error when the generator returns"
flags: [noStrict, async]
---*/

var it = {
	[Symbol.asyncIterator]() {
		return {
			next() { return { done: false }; },
			return() { throw "closed"; }
		};
	}
};
async function* g() { for await (var x of it) yield 1; }
var r = g();
r.next().then(() => r.return()).then(
	() => { throw new Test262Error("Expected iterator closing to reject"); },
	error => { assert.sameValue(error, "closed"); }
).then(() => $DONE(), $DONE);
