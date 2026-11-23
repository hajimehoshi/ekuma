// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Implicit iterator steps preserve the native next call for getters and generators"
flags: [noStrict]
---*/

function lengthGetter() {
	assert.sameValue(lengthGetter.caller, null);
	return 1;
}

function entries() {
	const arrayLike = Object.defineProperty({0: 42}, "length", {get: lengthGetter});
	for (const [key, value] of Array.prototype.entries.call(arrayLike)) {
		return value;
	}
}
assert.sameValue(entries(), 42);

function elementGetter() {
	assert.sameValue(elementGetter.caller, null);
	return new Error().stack.split("\n")[2];
}

function destructure() {
	const [, value] = [0, ,];
	return value;
}

function spread() {
	return [...[0, ,]][1];
}

Object.defineProperty(Array.prototype, "1", {get: elementGetter, configurable: true});
try {
	const iterator = [0, ,].values();
	iterator.next();
	const frame = iterator.next().value;
	assert.sameValue(destructure(), frame);
	assert.sameValue(spread(), frame);
} finally {
	delete Array.prototype[1];
}

function* generator() {
	yield new Error().stack.split("\n")[2];
}

function generatorFrame() {
	for (const frame of generator()) return frame;
}
assert.sameValue(generatorFrame(), generator().next().value);
