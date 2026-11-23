// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Borrowed Map and Set iterator next methods yield entry arrays in the entries factory's realm"
---*/

const foreign = $262.createRealm().global;
function check(collection, entries) {
	const iterator = entries.call(collection);
	const next = Object.getPrototypeOf(collection.entries()).next;
	const result = next.call(iterator);
	assert.sameValue(Object.getPrototypeOf(result), foreign.Object.prototype);
	assert.sameValue(Object.getPrototypeOf(result.value), foreign.Array.prototype);
}

check(new Map([[1, 2]]), foreign.Map.prototype.entries);
check(new Set([3]), foreign.Set.prototype.entries);
