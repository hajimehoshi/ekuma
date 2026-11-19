// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "destructuring writes a binding only when its get or iterator step completes, keeps the TDZ until then, and stores undefined after the iterator is done"
---*/

function throwsReference(f) {
	try {
		f();
	} catch (e) {
		return e instanceof ReferenceError;
	}
	return false;
}

function iterable(values, onNext) {
	return {[Symbol.iterator]() {
		var i = 0;
		return {next() {
			onNext();
			return i < values.length ? {value: values[i++], done: false} : {done: true};
		}};
	}};
}

// A getter or an iterator step that reads the binding being declared sees the TDZ.
assert.sameValue(throwsReference(() => {
	let {a: x} = {get a() { return read(); }};
	function read() { return x; }
}), true);
assert.sameValue(throwsReference(() => {
	let [x] = iterable([1], () => read());
	function read() { return x; }
}), true);

// A getter or an iterator step sees the old value of an assigned variable, and one that throws
// leaves it alone.
assert.sameValue((function() {
	var x = 1, seen;
	({a: x} = {get a() { seen = x; return 2; }});
	return [seen, x].join();
})(), "1,2");
assert.sameValue((function() {
	var x = 1, seen;
	[x] = iterable([2], () => { seen = x; });
	return [seen, x].join();
})(), "1,2");
assert.sameValue((function() {
	let x = 1;
	try {
		({a: x} = {get a() { throw 0; }});
	} catch {}
	try {
		[x] = iterable([], () => { throw 0; });
	} catch {}
	return x;
})(), 1);

// A const binding cannot be assigned.
assert.sameValue((function() {
	const c = 1;
	try {
		({a: c} = {a: 2});
	} catch (e) {
		return [e instanceof TypeError, c].join();
	}
})(), "true,1");

// The step after the iterator is done stores undefined, also for an array iterated by index.
assert.sameValue((function() {
	var x = 1, y = 2, z = 3;
	[x, y, z] = [5];
	return [x, y, z].map(String).join();
})(), "5,undefined,undefined");
assert.sameValue((function() {
	let x = 1, y = 2, z = 3;
	[x, y, z] = iterable([5], () => {});
	return [x, y, z].map(String).join();
})(), "5,undefined,undefined");
assert.sameValue((function() {
	var a = 1, b = 2;
	[a, b] = [b, a];
	return [a, b].join();
})(), "2,1");

// Closures see the stored values, one binding per iteration of a loop.
assert.sameValue((function() {
	let x;
	const get = () => x;
	({a: x} = {a: 3});
	const fs = [];
	for (const [k, v] of new Map([["a", 1], ["b", 2]])) {
		fs.push(() => k + v);
	}
	return [get(), ...fs.map(f => f())].join();
})(), "3,a1,b2");

assert.sameValue((function({a: x}, [y, z]) {
	return [x, y, String(z)].join();
})({a: 1}, [2]), "1,2,undefined");
