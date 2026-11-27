// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a[++i] and a[i++] as callees, as values of their own counter, in compound assignments and nested in other reads use each key once and the counter in order"
---*/

var methods = [
	function () { return "m0 " + this.length; },
	function () { return "m1 " + (this === methods); },
	function () { return "m2"; },
	function () { return "m3"; },
];
function calls(i) { return [methods[i++](), methods[++i](), i]; }
// The read's value replaces the counter that the increment wrote.
function ownCounterPostfix(a, i) { i = a[i++]; return i; }
function ownCounterPrefix(a, i) { i = a[++i]; return i; }
function assignment(a, i) {
	var r = a[i++] += a[i++];
	return a.join() + " r=" + r + " i=" + i;
}
function nested(a, i) { return [a[++i][++i], a[i++][i++], i].join(); }
for (var n = 0; n < 3; n++) {
	assert.sameValue(calls(0).join(), "m0 4,m2,2");
	assert.sameValue(calls(1).join(), "m1 true,m3,3");
	assert.sameValue(ownCounterPostfix([10, 20], 0), 10);
	assert.sameValue(ownCounterPrefix([10, 20], 0), 20);
	assert.sameValue(assignment([1, 2, 3], 0), "3,2,3 r=3 i=2");
	assert.sameValue(nested([[1, 2], [3, 4, 5], [6, 7, 8, 9], [10]], -1), "2,5,3");
}
