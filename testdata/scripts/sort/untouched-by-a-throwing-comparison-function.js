// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a sort much longer than a run of insertion leaves the array untouched when the comparison function throws"
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

var a = items(1000), before = a.map(x => x.id).join(), calls = 0;
try {
	a.sort((x, y) => {
		if (++calls === 3000) {
			throw new Error("stop");
		}
		return x.key - y.key;
	});
} catch (e) {
}
assert.sameValue(calls, 3000);
assert.sameValue(a.map(x => x.id).join(), before);
