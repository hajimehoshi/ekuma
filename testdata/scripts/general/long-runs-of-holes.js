// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "long runs of holes"
flags: [noStrict]
---*/

var n = 200000;
function holes() {
	var a = new Array(n).fill(0);
	for (var i = 0; i < n; i++) if (i !== 3 && i !== 150000) delete a[i];
	return a;
}
function keys(a) {
	var ks = [];
	a.forEach((v, i) => ks.push(i));
	return ks.join();
}
var a = holes();
assert.sameValue(keys(a), "3,150000");
assert.sameValue(a.indexOf(0, 4), 150000);
assert.sameValue(a.indexOf(1), -1);
assert.sameValue(a.lastIndexOf(0, 149999), 3);
assert.sameValue(a.lastIndexOf(0, 2), -1);
assert.sameValue(a.includes(1), false);
assert.sameValue(a.reduceRight((x, v, i) => x + i, ""), "1500003");
assert.sameValue(keys(a.slice(1)), "2,149999");
assert.sameValue(keys([1].concat(a)), "0,4,150001");
assert.sameValue(a.flat().length, 2);
assert.sameValue(a.join("").length, 2);
assert.sameValue(a.join().lastIndexOf("0"), 150001);
assert.sameValue(keys(holes().reverse()), "49999,199996");
assert.sameValue(keys(holes().sort()), "0,1");
assert.sameValue(keys(holes().copyWithin(1, 4)), "149997");
a = holes();
a.length++;
a.shift();
assert.sameValue(keys(a), "2,149999");
a.splice(1, 1);
assert.sameValue(keys(a), "1,149998");
a.unshift(1, 2);
assert.sameValue(keys(a), "0,1,3,150000");
