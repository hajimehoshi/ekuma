// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A borrowed return method closes in its own realm before the first yield and in the helper's realm afterwards"
---*/

const other = $262.createRealm();
const makeHelper = other.evalScript(`(function () {
	return Iterator.prototype.take.call({
		next() { return { value: 1, done: false }; },
		return() { return 0; },
	}, 2);
})`);
const close = Iterator.zip([]).return;

function closeError(helper) {
	try {
		close.call(helper);
	} catch (e) {
		return Object.getPrototypeOf(e);
	}
}

assert.sameValue(closeError(makeHelper()), TypeError.prototype, "before the first yield");

const started = makeHelper();
started.next();
assert.sameValue(closeError(started), other.global.TypeError.prototype, "after the first yield");
