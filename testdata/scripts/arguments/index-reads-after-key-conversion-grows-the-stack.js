// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: Indexed reads retain their result when converting the key grows the stack
---*/

function noop() {}
function keyWithCall(count) {
	return { toString() { noop(...Array(count)); return "0"; } };
}
function readArgument(value, key) {
	const result = arguments[key];
	noop();
	return result;
}
function readElement(values, key) {
	const result = values[key];
	noop();
	return result;
}

assert.sameValue(readArgument(42, keyWithCall(8192)), 42);
assert.sameValue(readElement([17], keyWithCall(32768)), 17);
