// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Code that await or the return of an async function runs by reading constructor or then sees the frame of the function, which hides the caller of the function as any async function does"
flags: [async, noStrict]
---*/

let made, frame, caller;
// read records its first call, as a promise that await wraps is read again by a later job.
function read() {
	if (frame) {
		return;
	}
	frame = new Error().stack.split("\n")[2];
	caller = read.caller;
}

// The frame of the function during the read has the position of the call that made the operand.
function thenable() {
	made = new Error().stack.split("\n")[2];
	return Object.defineProperty({}, "then", { get: read });
}

function promise() {
	made = new Error().stack.split("\n")[2];
	return Object.defineProperty(Promise.resolve(), "constructor", { get: read });
}

function wrappedPromise() {
	made = new Error().stack.split("\n")[2];
	const p = Promise.resolve();
	p.constructor = null;
	return Object.defineProperty(p, "then", { get: read });
}

async function awaitsWrapped() {
	await wrappedPromise();
}

async function awaits() {
	await thenable();
}

async function awaitsLater() {
	await null;
	await promise();
}

async function returns() {
	return thenable();
}

async function returnsLater() {
	await null;
	return thenable();
}

async function* generates() {
	await thenable();
}

// A read that throws continues the function at its await.
async function catches() {
	try {
		await Object.defineProperty(Promise.resolve(), "constructor", { get() { throw "thrown"; } });
	} catch (e) {
		return e;
	}
}

function call(f) {
	return f === generates ? f().next() : f();
}

(async () => {
	for (const f of [awaits, awaitsLater, awaitsWrapped, returns, returnsLater, generates]) {
		made = frame = caller = undefined;
		await call(f);
		assert.sameValue(made.includes("at " + f.name + " "), true, f.name);
		assert.sameValue(frame, made, f.name);
		assert.sameValue(caller, null, f.name);
	}
	assert.sameValue(await catches(), "thrown");
})().then($DONE, $DONE);
