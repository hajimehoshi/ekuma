// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A delegated return closes the enclosing destructuring iterator before finally"
---*/

const log = [];
const source = {
	[Symbol.iterator]() { return this; },
	next() { return {done: false}; },
	return() { log.push("close"); return {}; },
};
function* values() {
	try {
		const [x = yield* [1]] = source;
	} finally {
		log.push("finally");
		yield 2;
	}
}
const it = values();
assert.sameValue(it.next().value, 1);
assert.sameValue(it.return(3).value, 2);
assert.sameValue(log.join(), "close,finally");
const result = it.next();
assert.sameValue(result.value, 3);
assert.sameValue(result.done, true);
