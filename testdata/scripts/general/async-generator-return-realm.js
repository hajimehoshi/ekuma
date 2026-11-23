// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A borrowed async generator return awaits in different realms at a yield and after completion"
flags: [async]
---*/

const other = $262.createRealm();
const gen = other.evalScript("(async function* () { yield 1; })()");
const close = (async function* () {})().return;
const value = Promise.resolve(42);
const marker = {};
Object.defineProperty(value, "then", { get() { throw marker; } });

(async () => {
	await gen.next();
	// At the yield, the generator's PromiseResolve reads the foreign promise's then.
	const request = close.call(gen, value);
	assert.sameValue(Object.getPrototypeOf(request), Promise.prototype);
	await request.then(
		() => { throw new Test262Error("expected return to reject"); },
		error => assert.sameValue(error, marker)
	);

	// After completion, the return method's PromiseResolve recognizes its own promise.
	const closing = close.call(gen, value);
	const queued = gen.next();
	const completed = await closing;
	assert.sameValue(completed.value, 42);
	assert.sameValue(Object.getPrototypeOf(completed), Object.prototype);
	// The return continuation drains the queued next request in the return method's realm.
	assert.sameValue(Object.getPrototypeOf(await queued), Object.prototype);
})().then(() => $DONE(), $DONE);
