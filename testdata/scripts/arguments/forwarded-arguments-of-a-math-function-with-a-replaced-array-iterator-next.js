// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a call that forwards the arguments to a Math function while the next method of array iterators is replaced"
---*/

function f() {
  var v = "local";
  var r = Math.abs(...arguments);
  return [r, v];
}

var prototype = Object.getPrototypeOf([][Symbol.iterator]());
var next = prototype.next;
var calls = 0;
var got;
try {
  prototype.next = function () {
    calls++;
    return next.call(this);
  };
  got = f(-2);
} finally {
  prototype.next = next;
}
assert.sameValue(got[0], 2);
assert.sameValue(got[1], "local");
assert.sameValue(calls, 2);
