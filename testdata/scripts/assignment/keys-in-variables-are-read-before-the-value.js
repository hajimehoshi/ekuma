// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a key in a variable, or updated in place, is read before the value changes the variable, and converted once"
---*/

assert.sameValue((function() {
	let a = [1, 2, 3], i = 0;
	a[++i] += (i = 5, 10);
	return [a.join(), i].join("|");
})(), "1,12,3|5");

assert.sameValue((function() {
	let a = [], i = 0;
	a[++i] = (i = 5);
	return [a[1], a[5], i].join();
})(), "5,,5");

assert.sameValue((function() {
	let n = 0, k = {toString() { n++; return "p"; }}, o = {p: 1};
	o[k] += 1;
	o[k]++;
	o[k] ||= 9;
	return [o.p, n].join();
})(), "3,3");
