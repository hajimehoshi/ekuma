// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Reading a delegated result can resume the generator before value throws in the consumer"
---*/

function check(forwardNext) {
	const events = [];
	const returnValue = {}, getterError = {};
	let closing;
	const result = {
		get done() {
			events.push("done");
			// yield* reads done first. Destructuring reads it again after suspension.
			if (events.length === 2) closing = gen.return(returnValue);
			return false;
		},
		get value() {
			events.push("value");
			throw getterError;
		},
	};
	const gen = (function* () {
		try {
			yield* {
				[Symbol.iterator]() { return this; },
				next() { return result; },
			};
		} finally {
			events.push("finally");
			yield "cleanup";
		}
	})();
	if (forwardNext) {
		const next = gen.next;
		gen.next = function () { return next.call(this); };
	}

	let caught;
	try { const [value] = gen; } catch (e) { caught = e; }
	assert.sameValue(events.join(), "done,done,finally,value");
	assert.sameValue(caught, getterError);
	assert.sameValue(closing.value, "cleanup");
	assert.sameValue(closing.done, false);
	const end = gen.next();
	assert.sameValue(end.value, returnValue);
	assert.sameValue(end.done, true);
}

check(false);
check(true);
