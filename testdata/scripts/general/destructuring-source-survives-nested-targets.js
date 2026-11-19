// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "destructuring source survives nested targets"
flags: [noStrict]
---*/

assert.sameValue((function () {
	let o = [[9], 2], b;
	[o, b] = o;
	return b;
})(), 2, "an array pattern assigns the source");

assert.sameValue((function () {
	var o = { a: { b: 1 }, c: 4 };
	var { a: { b: o }, c } = o;
	return c;
})(), 4, "a nested pattern assigns the source");

assert.sameValue((function () {
	let o = { a: undefined, b: 2 }, b;
	({ a: o = { b: 1 }, b } = o);
	return b;
})(), 2, "the target of a default is the source");

assert.sameValue((function () {
	let o = { a: undefined, b: 2 }, b;
	({ a: b = (o = { b: 1 }), b } = o);
	return b;
})(), 2, "a default assigns the source");
