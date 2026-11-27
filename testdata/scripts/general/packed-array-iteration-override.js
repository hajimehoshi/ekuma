// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "spreading a numeric array respects an overridden iterator"
---*/

const a = [];
for (let i = 0; i < 20; i++) a[i] = i;
a[Symbol.iterator] = function* () {
	yield "first";
	yield 42;
};

const copy = [...a];
assert.sameValue(copy.length, 2);
assert.sameValue(copy[0], "first");
assert.sameValue(copy[1], 42);
