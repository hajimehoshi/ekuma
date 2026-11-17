// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Sorting a large typed array orders numbers, signed zeros, and NaNs"
flags: [noStrict]
---*/

const a = new Float64Array(200001);
for (let i = 0; i < a.length; i++) {
	a[i] = i % 1000 === 0 ? NaN : i % 1000 === 1 ? -0 : Math.round(Math.random() * 1000) - 500;
}
a.sort();
let ok = true;
for (let i = 1; i < a.length; i++) {
	const x = a[i - 1], y = a[i];
	if (x > y || x !== x && y === y || Object.is(x, 0) && Object.is(y, -0)) {
		ok = false;
	}
}
assert.sameValue(ok && Number.isNaN(a[a.length - 1]) && !Number.isNaN(a[a.length - 202]), true);
