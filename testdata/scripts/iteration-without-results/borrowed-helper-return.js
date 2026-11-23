// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A borrowed return method closes in its own realm before the first yield and in the helper's realm afterwards"
includes: [realm-assertions.js]
---*/

const B = $262.createRealm();
const C = $262.createRealm();
const check = realmAssertions({ A: $262, B, C });
const close = B.evalScript("Iterator.zip([]).return");
const makeHelper = C.evalScript(`(function () {
	return Iterator.prototype.take.call({
		next() { return { value: 1, done: false }; },
		return() { return 0; },
	}, 2);
})`);

const fresh = makeHelper();
check.throws(() => close.call(fresh), "B", "TypeError", "borrowed return before first yield: close error");

const started = makeHelper();
started.next();
check.throws(() => close.call(started), "C", "TypeError", "borrowed return after first yield: close error");
check.prototype(close.call(started), "B", "Object", "borrowed return: completed result");
