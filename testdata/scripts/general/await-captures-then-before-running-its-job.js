// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: Await captures a wrapped promise's then method before running it in a later job
flags: [async]
---*/

async function trace(constructor) {
	const events = [];
	const input = Promise.resolve("original");
	Object.defineProperty(input, "constructor", constructor);
	input.then = resolve => {
		events.push("then");
		resolve("wrapped");
	};

	const completion = (async () => {
		const value = await input;
		events.push(value);
	})();
	input.then = () => { throw new Test262Error("await read then again"); };
	events.push("returned");
	Promise.resolve().then(() => events.push("tick"));
	await completion;
	return events.join();
}

(async () => {
	// The getter forces the ordinary path through PromiseResolve.
	const ordinary = await trace({ get() { return null; } });
	assert.sameValue(ordinary, "returned,then,tick,wrapped");
	assert.sameValue(await trace({ value: null }), ordinary);
})().then(() => $DONE(), $DONE);
