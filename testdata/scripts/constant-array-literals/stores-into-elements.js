// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Stores into an array that a literal of constants made leave the next array of the literal unchanged"
---*/

function literal() { return [1, 2, 3, 4]; }

function check(change, expected) {
	var a = literal();
	change(a);
	assert.sameValue(a.join(), expected);
	assert.sameValue(literal().join(), "1,2,3,4");
}

check(function (a) { a[0] = 9; }, "9,2,3,4");
check(function (a) { var k = 1; a[k] = 9; }, "1,9,3,4");
check(function (a) { delete a[0]; }, ",2,3,4");
check(function (a) { Object.defineProperty(a, 1, { value: 9, writable: false }); }, "1,9,3,4");
check(function (a) { a.push(5); a[0] = 9; }, "9,2,3,4,5");
