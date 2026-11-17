// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "repeated deletes from one object"
flags: [noStrict]
---*/

var o = {a: 1, b: 2, c: 3, d: 4, e: 5};
function get(o) { return o.d; }
var out = [get(o)];
delete o.b; out.push(get(o));
delete o.a; o.f = 6; out.push(get(o));
delete o.e; delete o.d; out.push(get(o), Object.keys(o).join(), Object.values(o).join());
var a = []; a[100000] = 0;
for (var i = 0; i < 6; i++) a[i * 1000] = i;
delete a[2000]; out.push(Object.keys(a).join());
delete a[0]; a[500] = 7; out.push(Object.keys(a).join(), a[500], a[5000]);
assert.sameValue(String(out.join("|")), "4|4|4||c,f|3,6|0,1000,3000,4000,5000,100000|500,1000,3000,4000,5000,100000|7|5");
