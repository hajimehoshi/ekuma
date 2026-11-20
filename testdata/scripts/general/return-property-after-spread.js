// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: A property returned after a spread grows the stack survives the next call
---*/

const o = { x: 42, read() { return this.x; } };
function next() {}
function run(args) {
	var result = o.read(...args);
	next();
	return result;
}

run([]);
assert.sameValue(run(Array(8192)), 42);
