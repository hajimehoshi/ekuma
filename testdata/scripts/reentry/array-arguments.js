// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Array methods see an array that the conversion of an argument changes"
---*/

// check compares what f returns, as JSON with "undefined" for undefined, or the name of the error
// that it throws.
function check(name, expected, f) {
	let actual;
	try {
		actual = JSON.stringify(f(), (key, value) => value === undefined ? "undefined" : value);
	} catch (e) {
		actual = e.name;
	}
	assert.sameValue(actual, expected, name);
}

check("slice of a shortened array", '[1,2,"undefined","undefined"]', function () {
	const a = [1, 2, 3, 4];
	return a.slice({ valueOf() { a.length = 2; return 0; } });
});
check("slice of a lengthened array", "[1,2,3]", function () {
	const a = [1, 2, 3];
	return a.slice(0, { valueOf() { a.push(9); return 3; } });
});
check("splice of a lengthened array", '[[1,"x",4],[2,3]]', function () {
	const a = [1, 2, 3, 4];
	const removed = a.splice(1, { valueOf() { a.push(7); return 2; } }, "x");
	return [a, removed];
});
check("splice of a shortened array", '[[1,"x","undefined"],["undefined","undefined"],3]', function () {
	const a = [1, 2, 3, 4];
	const removed = a.splice({ valueOf() { a.length = 1; return 1; } }, 2, "x");
	return [a, removed, a.length];
});
check("toSpliced of a shortened array", '["x","undefined","undefined"]', function () {
	const a = [1, 2, 3];
	return a.toSpliced(0, { valueOf() { a.length = 1; return 1; } }, "x");
});
check("fill of a shortened array", "[[0,0,0,0],4]", function () {
	const a = [1, 2, 3, 4];
	a.fill(0, { valueOf() { a.length = 1; return 0; } });
	return [a, a.length];
});
check("fill of a frozen array", "TypeError", function () {
	const a = [1, 2, 3];
	a.fill(0, { valueOf() { Object.freeze(a); return 0; } });
});
check("copyWithin of a shortened array", '[[3,4,"undefined",4],4]', function () {
	const a = [1, 2, 3, 4, 5];
	a.copyWithin(0, { valueOf() { a.length = 4; return 2; } });
	return [a, a.length];
});
check("indexOf in a shortened array", "-1", function () {
	const a = [1, 2, 3];
	return a.indexOf(undefined, { valueOf() { a.length = 1; return 0; } });
});
check("includes in a shortened array", "true", function () {
	const a = [1, 2, 3];
	return a.includes(undefined, { valueOf() { a.length = 1; return 0; } });
});
check("lastIndexOf in a sparse array", "2", function () {
	const a = [1, 2, 3];
	return a.lastIndexOf(3, { valueOf() { a.length = 1; a[2] = 3; return 2; } });
});
check("join of a shortened array", '"a--"', function () {
	const a = ["a", "b", "c"];
	return a.join({ toString() { a.length = 1; return "-"; } });
});
check("join of a lengthened array", '"a-b-c"', function () {
	const a = ["a", "b", "c"];
	return a.join({ toString() { a.push("d"); return "-"; } });
});
check("length of a lengthened array", "[1,2]", function () {
	const a = [1, 2, 3];
	a.length = { valueOf() { a.push(4); return 2; } };
	return a;
});
check("length that becomes read-only", "TypeError", function () {
	"use strict";
	const a = [1, 2, 3];
	a.length = { valueOf() { Object.defineProperty(a, "length", { writable: false }); return 1; } };
});
