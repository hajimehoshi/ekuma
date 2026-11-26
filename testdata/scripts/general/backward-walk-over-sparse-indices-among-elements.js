// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "backward walk over sparse indices among the elements of an array"
---*/

var a = [0, 1, 2, 3, 4, 5, 6, 7];
Object.defineProperty(a, 2, { get() { return "x"; }, configurable: true });
Object.defineProperty(a, 5, { get() { return "y"; }, configurable: true });
a[1000] = "z";
delete a[3];
delete a[4];
delete a[7];

var visited = [];
a.reduceRight(function (_, v, i) {
  visited.push(i + ":" + v);
}, 0);
assert.sameValue(visited.join(), "1000:z,6:6,5:y,2:x,1:1,0:0");
assert.sameValue(a.lastIndexOf("y"), 5);
assert.sameValue(a.lastIndexOf("x"), 2);
