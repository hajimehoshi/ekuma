// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "an elision in array destructuring does not read the value of the iterator result"
flags: [noStrict]
---*/

function iter(log) {
	return {[Symbol.iterator]() { return {
		next() { log.push("n"); return {get done() { log.push("done"); return false; }, get value() { log.push("val"); return 1; }}; },
		return() { log.push("ret"); return {}; },
	}; }};
}
var out = [], log = [], a;
var [,] = iter(log); out.push(log.join()); log = [];
[a, , ] = iter(log); out.push(log.join()); log = [];
(function([, x]) {})(iter(log)); out.push(log.join()); log = [];
for (var [,] of [iter(log)]); out.push(log.join()); log = [];
try { var [,] = {[Symbol.iterator]() { return {next() { throw 1; }, return() { log.push("ret"); }}; }}; } catch (e) { log.push("caught"); }
var [, , b] = [1, 2, 3];
out.push(log.join(), b);
assert.sameValue(String(out.join("|")), "n,done,ret|n,done,val,n,done,ret|n,done,n,done,val,ret|n,done,ret|caught|3");
