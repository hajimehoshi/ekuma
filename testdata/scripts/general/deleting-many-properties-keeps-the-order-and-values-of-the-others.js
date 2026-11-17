// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "deleting many properties keeps the order and values of the others"
flags: [noStrict]
---*/

var o = {}, n = 20000;
for (var i = 0; i < n; i++) o["k" + i] = i;
for (var i = 0; i < n; i += 2) delete o["k" + i];
var odd = Object.keys(o);
o.k0 = "again";
delete o.k1;
Object.setPrototypeOf(o, {inherited: 1});
o.last = "x";
for (var i = 3; i < n - 100; i += 2) delete o["k" + i];
Object.freeze(o);
var keys = [];
for (var k in o) keys.push(k);
var a = [];
a[79999] = 0;
for (var i = 0; i < 8000; i++) a[(i * 7919) % 79999] = 8000 - i;
a.sort((x, y) => x - y);
assert.sameValue(String([odd.length, odd[0], odd[n / 2 - 1], keys.length, keys[0], keys.slice(-3).join("/"), o.k19999,
	JSON.stringify(o).length, Object.isFrozen(o), a[0], a[7999], a[8000], 8000 in a, a.length].join()), "10000,k1,k19999,53,k19901,k0/last/inherited,19999,775,true,0,7999,8000,true,80000");
