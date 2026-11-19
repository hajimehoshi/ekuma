// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "method call on a local keeps the object the method was read from"
---*/

function self() { return this; }

(function () {
  var o = {m: self};
  var original = o;
  assert.sameValue(o.m(o = {}), original, "an argument assigns the variable");
})();

(function () {
  var o = {get m() { o = {}; return self; }};
  var original = o;
  assert.sameValue(o.m(), original, "the getter of the method assigns the variable");
})();

(function () {
  var o = {m: self};
  // This call leaves the object in the register for the this value of the next call.
  o.m();
  o = null;
  var r = o?.m();
  assert.sameValue(r, undefined, "an optional call on a variable that is null");
})();
