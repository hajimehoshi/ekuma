// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "compound assignments and updates with a literal index key read and write the element of that index"
---*/

var log = [];
var a = [1, 2, 3, 4];
Object.defineProperty(a, 1, {
  get() { log.push("get"); return 10; },
  set(v) { log.push("set " + v); },
  configurable: true,
});
a[1] += 5;
a[0] *= 7;
a["2"]++;
--a[3];
assert.sameValue(log.join(), "get,set 15");
assert.sameValue([a[0], a[2], a[3]].join(), "7,4,3");

var t = new Int8Array(2);
t[1] -= 3;
assert.sameValue(t.join(), "0,-3");

var proto = {0: "p"};
var o = Object.create(proto);
o[0] += "q";
assert.sameValue([o[0], proto[0], o.hasOwnProperty(0)].join(), "pq,p,true");
