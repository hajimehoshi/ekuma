// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "for-of with two bindings over the entries of Maps, Sets and arrays"
flags: [noStrict]
---*/

var m = new Map([["a", 1], ["b", 2], ["c", 3]]);
var out = [];
for (const [k, v] of m) {
	out.push(k + v);
	if (k === "a") {
		m.delete("b");
		m.set("d", 4);
	}
}
assert.sameValue(out.join(), "a1,c3,d4", "a Map that changes during the loop");

out = [];
for (let [x, y] of new Set(["p", "q"]).entries()) {
	out.push(x + y);
}
assert.sameValue(out.join(), "pp,qq", "the entries of a Set");

out = [];
for (var [i, e] of ["u", "v", "w"].entries()) {
	if (i === 2) {
		break;
	}
	out.push(i + e);
}
assert.sameValue(out.join() + ":" + i + e, "0u,1v:2w", "array entries left by break");

var a, b;
out = [];
for ([a, b] of new Map([[5, 6]])) {
	out.push(a * b);
}
assert.sameValue(out.join() + ":" + a + b, "30:56", "assignment to variables");

var fns = [];
for (const [k, v] of new Map([["x", 1], ["y", 2]])) {
	fns.push(function () { return k + v; });
}
assert.sameValue(fns.map(function (f) { return f(); }).join(), "x1,y2", "bindings captured in each iteration");

out = [];
for (const [k, v, rest] of new Map([["r", 1]])) {
	out.push(typeof rest);
}
assert.sameValue(out.join(), "undefined", "a third binding");

out = [];
for (const [k, v] of [[1, 2], [3, 4]]) {
	out.push(k * v);
}
for (const [k, v] of Object.entries({p: 5})) {
	out.push(k + v);
}
assert.sameValue(out.join(), "2,12,p5", "arrays of pairs");
