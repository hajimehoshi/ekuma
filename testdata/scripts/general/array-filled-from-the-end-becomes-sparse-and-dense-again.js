// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "array filled from the end becomes sparse and dense again"
flags: [noStrict]
---*/

var a = []; for (var i = 99; i >= 0; i--) a[i] = i;
a[1000000] = "far";
var k = Object.keys(a);
var r = [a.length, k.length, k[99], k[100], 500 in a, a[1000000]];
a.length = 50;
k = Object.keys(a);
r.push(a.length, k.length, k[49], 1000000 in a, a[60]);
a[300] = 3;
r.push(a.length, Object.keys(a).slice(-2).join());
for (var i = 299; i >= 50; i--) a[i] = -i;
r.push(Object.keys(a).length, a.lastIndexOf(3), a[100], a.indexOf(-50));
assert.sameValue(String(r.join("|")), "1000001|101|99|1000000|false|far|50|50|49|false||301|49,300|301|300|-100|50");
