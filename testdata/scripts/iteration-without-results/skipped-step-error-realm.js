// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A skipped step throws in the realm of the borrowed next method"
---*/

const foreign = $262.createRealm().global;
const iterator = { next: new foreign.Map().entries().next };
let caught;
try {
	Iterator.prototype.drop.call(iterator, 1).next();
} catch (e) {
	caught = e;
}
assert.sameValue(caught instanceof foreign.TypeError, true);
