// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a member target keeps its object while a getter, an iterator step or an initializer reassigns the variable"
---*/

// it reassigns a when its first value is read.
function iterable(f) {
	return {[Symbol.iterator]() { return {next() { f(); return {value: 1, done: false}; }, return() { return {}; }}; }};
}

assert.sameValue((function() {
	var a = {}, old = a;
	Object.defineProperty(a, "k", {get() { a = {}; return 1; }, set(v) { this.v = v; }});
	a.k++;
	return [old.v, a.v].join();
})(), "2,");

assert.sameValue((function() {
	var a = {}, old = a;
	[a.k] = iterable(() => { a = {}; });
	return [old.k, a.k].join();
})(), "1,");

assert.sameValue((function() {
	var a = {}, old = a, j = "k";
	[a[j] = (a = {}, 1)] = [];
	return [old.k, a.k].join();
})(), "1,");

assert.sameValue((function() {
	var a = {}, old = a, j = "k";
	({p: a[j]} = {get p() { a = {}; return 1; }});
	return [old.k, a.k].join();
})(), "1,");

assert.sameValue((function() {
	var a = {}, old = a;
	for ([a.k] of [iterable(() => { a = {}; })]);
	return [old.k, a.k].join();
})(), "1,");
