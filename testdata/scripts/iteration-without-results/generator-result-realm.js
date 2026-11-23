// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A borrowed generator next method returns a yielded result from the generator's realm"
---*/

const other = $262.createRealm();
const gen = other.evalScript("(function* () { yield 1; })()");
const next = (function* () {})().next;
assert.sameValue(Object.getPrototypeOf(next.call(gen)), other.global.Object.prototype);
assert.sameValue(Object.getPrototypeOf(next.call(gen)), Object.prototype);

const result = { value: 2, done: false };
const delegate = other.evalScript("(function* (inner) { yield* inner; })");
const inner = {
	[Symbol.iterator]() { return this; },
	next() { return result; },
};
assert.sameValue(next.call(delegate(inner)), result);
