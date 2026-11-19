// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a sort much longer than a run of insertion is stable in the default order"
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
function stable(a) {
	for (var i = 1; i < a.length; i++) {
		var p = a[i - 1], q = a[i];
		if (p.key > q.key || p.key === q.key && p.id > q.id) {
			return "out of order at " + i;
		}
	}
	return "stable";
}

var a = items(1000);
for (var x of a) {
	x.toString = function () { return String(this.key); };
}
assert.sameValue(stable(a.sort()), "stable");
