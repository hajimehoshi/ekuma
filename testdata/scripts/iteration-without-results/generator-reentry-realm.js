// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A foreign generator resumed by for-of gets the borrowed next method's reentry error"
---*/

const other = $262.createRealm();
const make = other.evalScript(`(function* (ref) {
	try { ref.gen.next(); }
	catch (e) { yield [e]; }
})`);
const ref = {};
const gen = ref.gen = make(ref);
gen.next = (function* () {})().next;

let yielded;
for (const value of gen) yielded = value;
assert.sameValue(Object.getPrototypeOf(yielded), other.global.Array.prototype);
assert.sameValue(Object.getPrototypeOf(yielded[0]), TypeError.prototype);
