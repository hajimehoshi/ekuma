// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "An array length store truncates the array after the same site added an ordinary property"
---*/

function truncate(object) {
	object.length = 1;
}

const array = [10, 20];
truncate(Object.create(Array.prototype));
truncate(array);
assert.sameValue(array.length, 1);
assert.sameValue(1 in array, false);
