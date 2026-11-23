// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An async generator can resume after a return request is refused at the nesting limit"
flags: [async]
---*/

(async () => {
	const it = (async function* () { yield 1; yield 2; })();
	await it.next();
	const arg = Promise.resolve();
	Object.defineProperty(arg, "constructor", { get() { throw new Error(); } });
	let request;
	function* nest(n) {
		if (n === 0) return;
		const inner = nest(n - 1);
		try {
			inner.next();
		} catch (e) {
			// Unwrapping the return argument and resuming the body both fail at this depth.
			request = it.return(arg);
		}
	}
	nest(1024).next();
	assert.sameValue(request instanceof Promise, true);
	const error = await request.catch(e => e);
	assert.sameValue(error instanceof RangeError, true);
	assert.sameValue((await it.next()).value, 2);
})().then(() => $DONE(), $DONE);
