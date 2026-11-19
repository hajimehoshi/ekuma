// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a member assignment stores through the object and key evaluated before the value"
---*/

assert.sameValue((function() {
	let a = [], old = a, j = 0;
	a[j] = (j = 1, 5);
	return [old[0], old[1]].join();
})(), "5,");

assert.sameValue((function() {
	var a = {}, old = a;
	a.x = (a = {}, 5);
	return [old.x, a.x].join();
})(), "5,");

assert.sameValue((function() {
	var a = [], old = a, j = 0;
	a[j] = (j = 1, a = [], 5);
	return [old[0], old[1], a.length].join();
})(), "5,,0");

assert.sameValue((function() {
	let a = {}, old = a, k = "p";
	a[k] += (a = {}, k = "q", "v");
	return [old.p, old.q, a.p].join();
})(), "undefinedv,,");

assert.sameValue((function() {
	let a = {}, old = a, k = "p";
	a[k] ??= (a = {}, k = "q", "v");
	return [old.p, old.q, a.p].join();
})(), "v,,");

{
	let a = {}, old = a, j = 0;
	a[j] = (j = 1, a = {}, 5);
	assert.sameValue([old[0], old[1], a[0]].join(), "5,,");
}
