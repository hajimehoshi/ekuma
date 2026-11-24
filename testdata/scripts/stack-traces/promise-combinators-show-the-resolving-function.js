// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Promise.all and Promise.allSettled show the frame of the resolving function where it reads then, unlike Promise.resolve and reactions"
flags: [async]
---*/

let frames;
function read() {
	frames = new Error().stack.split("\n").slice(2);
}

// seen returns what the stack trace shows below the read of then by a call of f.
function seen(f) {
	frames = undefined;
	Object.defineProperty(Array.prototype, "then", { get: read, configurable: true });
	try {
		f();
	} finally {
		delete Array.prototype.then;
	}
	return frames;
}

let resolve;
new Promise((r) => { resolve = r; });
const resolving = seen(() => resolve([]))[0];

for (const name of ["all", "allSettled"]) {
	const got = seen(() => Promise[name]([]));
	assert.sameValue(got[0], resolving, name);
	assert.sameValue(got[1].includes("Promise." + name), true, name);
}

assert.sameValue(seen(() => Promise.resolve([]))[0].includes("Promise.resolve"), true);

const result = {};
Object.defineProperty(result, "then", { get: read });
frames = undefined;
Promise.resolve().then(() => result).then(() => {
	assert.sameValue(frames.length, 0);
}).then($DONE, $DONE);
