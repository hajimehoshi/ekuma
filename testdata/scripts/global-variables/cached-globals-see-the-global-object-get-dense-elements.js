// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reads and assignments of a global variable that ran before the array index properties of the global object became elements see the property afterwards"
---*/

var x = 1;
function read() { return x; }
function write(v) { x = v; }
// An index far beyond the elements is stored like a named property, until enough elements
// come before it.
function addSparseThenDense(sparse, from, to) {
	globalThis[sparse] = 0;
	for (var i = from; i < to; i++) {
		globalThis[i] = i;
	}
}
addSparseThenDense(100, 0, 100);
write(2);
assert.sameValue(read(), 2);
addSparseThenDense(1000, 101, 300);
globalThis.x = 3;
assert.sameValue(read(), 3);
write(4);
assert.sameValue(globalThis.x, 4);
