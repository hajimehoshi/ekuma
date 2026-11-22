// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "stores to literal index keys reach the element of that index of every kind of object"
---*/

var strict = (function () { return this === undefined; })();

function refused(f) {
  try {
    f();
  } catch (e) {
    return e.constructor.name;
  }
  return "done";
}
var refusal = strict ? "TypeError" : "done";

var a = [0, 1, 2];
a[1] = "in bounds";
a[3] = "append";
a[5] = "beyond";
assert.sameValue([a.length, a[1], a[3], a[4], a[5]].join(), "6,in bounds,append,,beyond");

// The first store makes room for more elements.
var b = [];
b[0] = "x";
b[1] = "y";
b[2] = "z";
assert.sameValue([b.length, b.join()].join(), "3,x,y,z");

// An accessor leaves the indices of Array.prototype sparse for good, so this comes after the
// stores to arrays.
var holes = [, ,];
var log = [];
Object.defineProperty(Object.getPrototypeOf(holes), 1, {
  set(v) { log.push(v); },
  configurable: true,
});
holes[1] = "set";
delete Array.prototype[1];
assert.sameValue([log.join(), holes.hasOwnProperty(1)].join(), "set,false");

var t = new Uint8Array(2);
t[1] = 300;
t[2] = 9;
assert.sameValue([t.join(), t[2]].join(), "0,44,");

var frozen = Object.freeze([1]);
assert.sameValue(refused(function () { "use strict"; frozen[0] = 2; }), "TypeError");
assert.sameValue(refused(function () { frozen[0] = 2; }), refusal);
assert.sameValue(frozen[0], 1);
assert.sameValue(refused(function () { "abc"[0] = "x"; }), refusal);

var o = {};
o[0] = "zero";
o["7"] = "seven";
assert.sameValue(Object.keys(o).join(), "0,7");
assert.sameValue([o[0], o[7]].join(), "zero,seven");
