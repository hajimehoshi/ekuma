// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "computed super property evaluation order"
flags: [noStrict]
---*/

var log = [];
var p1 = { get p() { log.push("get1"); return 1; }, set p(v) { log.push("set1:" + v); } };
var p2 = { get p() { log.push("get2"); return 2; }, set p(v) { log.push("set2:" + v); } };
var obj = { __proto__: p1,
	read() { super[key]; },
	assign() { super[key] = rhs(); },
	compound() { super[key] += rhs(); } };
var key = { toString() { log.push("key"); Object.setPrototypeOf(obj, p2); return "p"; } };
function rhs() { log.push("rhs"); Object.setPrototypeOf(obj, p2); return 10; }
for (var m of ["read", "assign", "compound"]) { Object.setPrototypeOf(obj, p1); obj[m](); }
assert.sameValue(String(log.join()), "key,get1,rhs,key,set1:10,key,get1,rhs,set1:11");
