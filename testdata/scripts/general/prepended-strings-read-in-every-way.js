// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prepended strings read in every way"
flags: [noStrict]
---*/

function pre(s) {
	var cs = Array.from(s), r = "";
	for (var i = cs.length; i-- > 0;) r = cs[i] + r;
	return r;
}
var d = "9876543210".repeat(4);
var o = {};
o[d] = 1;
var o2 = {};
o2[pre(d)] = 2;
assert.sameValue(String([pre(d).length, o[pre(d)], o2[d], pre(d) in o, o.hasOwnProperty(pre(d)),
	new Map([[d, "m"]]).get(pre(d)), new Set([pre(d)]).has(d),
	pre(d) === d, d === pre(d), pre(d) == d, "9876" < pre(d), pre(d) < "9877",
	[pre(d), "99", "9"].sort()[1] === d,
	pre(d)[1], pre(d).charCodeAt(39), pre(d).at(-2), pre(d).slice(10, 13), pre(d).substring(37),
	pre(d).indexOf("10", 5), pre(d).lastIndexOf("98"), pre(d).startsWith("987"), pre(d).split("0").length,
	/(\d)0(\d)/.exec(pre(d)).index, pre(d).replace(/0/g, "").length, pre(d).match(/9/g).length,
	JSON.stringify(pre(d)) === '"' + d + '"', JSON.parse(pre('{"a":[1,2,3],"b":"xyzxyzxyzxyzxyz"}')).b,
	+pre("1234567890123456789012345678901234"), BigInt(pre(d)) === BigInt(d), pre(d) == BigInt(d),
	BigInt(d) - 1n < pre(d), eval(pre("1+2+3+4+5+6+7+8+9+10+11+12+13+14")),
	(0, eval)(pre("'abcdefghij' + 'klmnopqrstuvwxyz'.length"))].join(" ")), "40 1 2 true true m true true true true true true true 8 48 1 987 210 8 30 true 5 8 36 4 true xyzxyzxyzxyzxyz 1.2345678901234568e+33 true true true 105 abcdefghij16");
