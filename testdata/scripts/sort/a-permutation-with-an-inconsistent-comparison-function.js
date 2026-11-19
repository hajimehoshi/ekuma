// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a sort much longer than a run of insertion gives a permutation with an inconsistent comparison function"
flags: [noStrict]
---*/

var seed = 1;
function next() {
	seed = (seed * 1103515245 + 12345) % 2147483648;
	return seed;
}
function items(n) {
	var a = [];
	for (var i = 0; i < n; i++) {
		a.push({key: next() % 7, id: i});
	}
	return a;
}

var a = items(1000);
a.sort(() => next() % 3 - 1);
var ids = a.map(x => x.id).sort((x, y) => x - y);
assert.sameValue(ids.every((x, i) => x === i), true);
