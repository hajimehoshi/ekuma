// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An inner flatMap close error survives reentry from the outer close method"
---*/

const events = [];
const innerError = {};
let reentryError;
const inner = {
	next() { return {value: 1, done: false}; },
	get return() {
		events.push("inner");
		throw innerError;
	},
};
const outer = {
	next() { return {done: false}; },
	return() {
		events.push(this === outer ? "outer" : "wrong receiver");
		try { helper.next(); } catch (e) {
			reentryError = e;
			throw e;
		}
	},
};
const helper = Iterator.prototype.flatMap.call(outer, () => inner);
helper.next();
let caught;
try { helper.return(); } catch (e) { caught = e; }
assert.sameValue(events.join(), "inner,outer");
assert.sameValue(reentryError instanceof TypeError, true);
assert.sameValue(caught, innerError);
assert.sameValue(helper.next().done, true);
