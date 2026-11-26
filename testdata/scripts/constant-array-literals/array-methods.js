// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Array methods that change an array that a literal of constants made leave the next array of the literal unchanged"
---*/

function literal() { return [4, 3, 2, 1]; }

// The literal has more elements than an array shifts by copying them.
function long() {
	return [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
		20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39];
}

function check(make, change, expected) {
	var a = make();
	var before = make().join();
	change(a);
	assert.sameValue(a.join(), expected);
	assert.sameValue(make().join(), before);
}

check(literal, function (a) { a.pop(); a.push(9); }, "4,3,2,9");
check(literal, function (a) { a.shift(); a.push(9); }, "3,2,1,9");
check(long, function (a) { a.shift(); a.push(9); a[0] = 9; }, long().slice(1).join().replace(/^1/, "9") + ",9");
check(literal, function (a) { a.unshift(9); }, "9,4,3,2,1");
check(literal, function (a) { a.splice(1, 1, 9); }, "4,9,2,1");
check(literal, function (a) { a.reverse(); }, "1,2,3,4");
check(literal, function (a) { a.fill(9, 2); }, "4,3,9,9");
check(literal, function (a) { a.sort(); }, "1,2,3,4");
check(literal, function (a) { a.length = 1; a.push(9); }, "4,9");

// The storage of ten elements has room for an eleventh, which two arrays of the literal must not
// share.
function ten() { return [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]; }
var a = ten(), b = ten();
a.push(10);
b.push(11);
assert.sameValue(a[10], 10);
