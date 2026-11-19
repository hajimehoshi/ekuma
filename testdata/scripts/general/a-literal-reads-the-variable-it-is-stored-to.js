// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "an object, array or template literal stored to a variable reads the variable's old value or TDZ, and fields read a variable as it is when they are defined"
---*/

function throwsReference(f) {
	try {
		f();
	} catch (e) {
		return e instanceof ReferenceError;
	}
	return false;
}

assert.sameValue(throwsReference(() => { let o = {a: o}; }), true);
assert.sameValue(throwsReference(() => { const o = {o}; }), true);
assert.sameValue(throwsReference(() => { let a = [a]; }), true);
assert.sameValue(throwsReference(() => { const s = `${s}`; }), true);

assert.sameValue((function() {
	var o = {v: 1};
	o = {prev: o, v: 2};
	let a = [1];
	a = [a, ...a];
	let s = "a";
	s = `${s}-${{toString() { s = "z"; return "y"; }}}`;
	return [o.prev.v, o.v, a[0][0], a[1], s].join();
})(), "1,2,1,1,a-y");

assert.sameValue((function() {
	const o = {m() { return o; }};
	return o.m() === o;
})(), true);

// A computed key, a spread getter or a property value can reassign a variable between fields.
assert.sameValue((function() {
	var x = 1;
	var src = {get g() { x = 3; return 0; }};
	var o = {x, [(x = 2, "b")]: x, c: x, ...src, d: x, e: (x = 4), x2: x};
	return [o.x, o.b, o.c, o.d, o.e, o.x2].join();
})(), "1,2,2,3,4,4");
