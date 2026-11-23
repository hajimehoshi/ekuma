// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A foreign generator resumed by for-of gets the borrowed next method's reentry error"
includes: [realm-assertions.js]
---*/

const method = $262.createRealm();
const receiver = $262.createRealm();
const check = realmAssertions({ caller: $262, method, receiver });
const next = method.evalScript("(function* () {})().next");
const make = receiver.evalScript(`(function* (ref, next) {
	try { next.call(ref.gen); }
	catch (e) { yield [e]; }
})`);
const ref = {};
const gen = ref.gen = make(ref, next);
// The optimized for-of step requires the caller's intrinsic next.
gen.next = (function* () {})().next;

let yielded;
for (const value of gen) yielded = value;
check.prototype(yielded, "receiver", "Array", "yielded array");
check.prototype(yielded[0], "method", "TypeError", "reentrant next");
