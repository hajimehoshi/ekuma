// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An async generator awaits a promise in its own realm when next is borrowed"
flags: [async]
---*/

const other = $262.createRealm();
const make = other.evalScript("(async function* (value) { yield value; yield 7; })");
const value = other.global.Promise.resolve(42);
Object.defineProperty(value, "then", {
	get() { throw new Test262Error("await should recognize the generator realm's promise"); },
});
const gen = make(value);
const next = (async function* () {})().next;

(async () => {
	const yielded = await next.call(gen);
	assert.sameValue(yielded.value, 42);
	assert.sameValue(Object.getPrototypeOf(yielded), other.global.Object.prototype);

	const primitive = await next.call(gen);
	assert.sameValue(Object.getPrototypeOf(primitive), other.global.Object.prototype);

	const completed = await next.call(gen);
	assert.sameValue(Object.getPrototypeOf(completed), Object.prototype);
})().then(() => $DONE(), $DONE);
