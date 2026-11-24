// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A throw queued by a return thenable reaches the yield in finally"
flags: [async]
---*/

(async () => {
	const events = [];
	const returnValue = {}, throwValue = {};
	let caught, queued;
	const gen = (async function* () {
		try {
			yield 1;
		} finally {
			events.push("finally");
			try {
				yield "cleanup";
			} catch (e) {
				caught = e;
			}
		}
	})();
	await gen.next();
	const closing = gen.return({
		get then() {
			events.push("get then");
			queued = gen.throw(throwValue);
			return resolve => {
				events.push("resolve");
				resolve(returnValue);
			};
		},
	});
	const [cleanup, end] = await Promise.all([closing, queued]);
	assert.sameValue(events.join(), "get then,resolve,finally");
	assert.sameValue(cleanup.value, "cleanup");
	assert.sameValue(cleanup.done, false);
	assert.sameValue(caught, throwValue);
	// Catching the queued throw lets finally finish the original return.
	assert.sameValue(end.value, returnValue);
	assert.sameValue(end.done, true);
})().then(() => $DONE(), $DONE);
