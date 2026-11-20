// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Map entry pairs belong to the realm of the next method"
---*/

const foreign = $262.createRealm().global;
const map = new foreign.Map([[1, 2]]);
const prototype = foreign.Array.prototype;
assert.sameValue(Object.getPrototypeOf(map.entries().next().value), prototype);
assert.sameValue(Object.getPrototypeOf([...map][0]), prototype);
assert.sameValue(Object.getPrototypeOf(Array.from(map)[0]), prototype);
for (const pair of map) {
	assert.sameValue(Object.getPrototypeOf(pair), prototype);
}
