// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "an assignment to a global variable that ran before the property became read-only does not write it"
flags: [noStrict]
---*/

var frozen = 1;
function write(v) { frozen = v; }
function writeStrict(v) { "use strict"; frozen = v; }
write(2);
writeStrict(3);
assert.sameValue(frozen, 3);
Object.defineProperty(globalThis, "frozen", { writable: false });
write(4);
assert.sameValue(frozen, 3);
var caught = "";
try {
	writeStrict(5);
} catch (e) {
	caught = e.constructor.name;
}
assert.sameValue(caught, "TypeError");
assert.sameValue(frozen, 3);
