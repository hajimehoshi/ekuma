// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a function continues with its variables after the delete operator called a trap of the object of a with statement"
flags: [noStrict]
---*/

var deleted = 0;
var scope = new Proxy({ x: 1 }, {
  deleteProperty(target, key) {
    deleted++;
    return delete target[key];
  },
});
function f() {
  var v = "local";
  var r;
  with (scope) {
    r = delete x;
  }
  return [r, v];
}
var got = f();
assert.sameValue(got[0], true);
assert.sameValue(got[1], "local");
assert.sameValue(deleted, 1);
