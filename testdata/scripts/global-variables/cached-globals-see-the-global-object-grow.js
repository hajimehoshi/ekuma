// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reads and assignments of a global variable that ran before the global object grew see the property afterwards"
---*/

var x = 1;
function read() { return x; }
function write(v) { x = v; }
// Enough properties to give the global object a shape of its own.
for (var i = 0; i < 200; i++) {
	globalThis["before" + i] = i;
}
write(2);
assert.sameValue(read(), 2);
// The global object needs more room for its properties as it grows.
for (var i = 0; i < 2000; i++) {
	globalThis["after" + i] = i;
}
globalThis.x = 3;
assert.sameValue(read(), 3);
write(4);
assert.sameValue(globalThis.x, 4);
