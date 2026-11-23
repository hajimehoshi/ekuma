// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An array length store truncates the array after the same site added an ordinary property"
---*/

function truncate(object) {
	object.length = 1;
}

function check(array) {
	truncate(Object.create(Object.getPrototypeOf(array)));
	truncate(array);
	assert.sameValue(array.length, 1);
	assert.sameValue(1 in array, false);
}

check([10, 20]);

// Change the attributes of a sparse index, then fill the array back to dense storage.
const array = [];
Object.setPrototypeOf(array, null);
Object.defineProperty(array, 63, {value: 63, configurable: true});
Object.defineProperty(array, 63, {writable: true, enumerable: true});
for (let i = 0; i < 63; i++) array[i] = i;
check(array);
