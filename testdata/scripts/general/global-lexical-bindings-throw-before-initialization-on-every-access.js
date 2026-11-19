// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "global lexical bindings throw before initialization on every access"
---*/

function read() {
	return x;
}

function write() {
	x = 2;
}

function throwsReferenceError(f) {
	try {
		f();
	} catch (e) {
		return e instanceof ReferenceError;
	}
	return false;
}

assert.sameValue(throwsReferenceError(read), true, "the first read");
assert.sameValue(throwsReferenceError(read), true, "the second read");
assert.sameValue(throwsReferenceError(write), true, "the first write");
assert.sameValue(throwsReferenceError(write), true, "the second write");

let x = 1;

assert.sameValue(read(), 1, "a read after the initialization");
write();
assert.sameValue(read(), 2, "a read after a write");
