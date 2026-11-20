// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the result of an assignment to a variable, used as an operand or assigned on, is the assigned value"
---*/

assert.sameValue((function() {
	let x = 0;
	const sum = (x = 1) + (x = 2);
	return [sum, x].join();
})(), "3,2");

assert.sameValue((function() {
	let x, o = {p: 1};
	const same = (x = o.p) === (x = 2, 1);
	return [same, x].join();
})(), "true,2");

assert.sameValue((function() {
	let a = 1, b = 2;
	a = (b = a + 10);
	return [a, b].join();
})(), "11,11");

assert.sameValue((function() {
	let a = 1, b = 0;
	a = (b ||= a + 1);
	return [a, b].join();
})(), "2,2");

assert.sameValue((function() {
	"use strict";
	let a = 1;
	try {
		a = (notDeclaredAnywhere = 5);
	} catch (e) {
		return [a, e.constructor.name].join();
	}
})(), "1,ReferenceError");
