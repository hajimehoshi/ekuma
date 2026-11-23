// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Destructuring an entry observes the array iterator of the borrowed entries factory's realm"
---*/

const foreign = $262.createRealm().global;
const entries = foreign.Map.prototype.entries.call(new Map([["key", "value"]]));
entries.next = new Map().entries().next;
foreign.Array.prototype[Symbol.iterator] = function* () {
	assert.sameValue(Object.getPrototypeOf(this), foreign.Array.prototype);
	yield this[1];
	yield this[0];
};
let seen;
for (const [a, b] of entries) {
	seen = a + "," + b;
}
assert.sameValue(seen, "value,key");
