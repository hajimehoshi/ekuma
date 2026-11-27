// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "indexed stores at the end of an ordinary object's dense elements"
flags: [noRepeat]
noRepeatReason: "the accessor leaves Object.prototype marked sparse"
---*/

function withRoom() {
  return {0: "first"};
}

function store(o, key, v) {
  o[key] = v;
}

var o = withRoom();
store(o, 1.5, "fraction");
assert.sameValue(o[1.5], "fraction");
assert.sameValue(Object.hasOwn(o, 1), false);

store(o, 1, "dynamic");
o[2] = "literal";
assert.sameValue(Object.keys(o).join(), "0,1,2,1.5");
assert.sameValue(o[1], "dynamic");
assert.sameValue(o[2], "literal");
assert.sameValue(Object.hasOwn(o, "length"), false);

o = Object.preventExtensions(withRoom());
var refused = (function () { return this; })() === undefined ? TypeError : undefined;
var caught;
try {
  o[1] = "refused";
} catch (e) {
  caught = e.constructor;
}
assert.sameValue(caught, refused);
assert.sameValue(Object.hasOwn(o, 1), false);

var proto = Object.defineProperty({}, 1, {value: "read-only"});
o = Object.setPrototypeOf(withRoom(), proto);
caught = undefined;
try {
  store(o, 1, "refused");
} catch (e) {
  caught = e.constructor;
}
assert.sameValue(caught, refused);
assert.sameValue(o[1], "read-only");

var received;
Object.defineProperty(Object.prototype, 1, {
  configurable: true,
  set(v) {
    received = v;
  },
});
try {
  o = withRoom();
  store(o, 1, "inherited");
  assert.sameValue(received, "inherited");
  assert.sameValue(Object.hasOwn(o, 1), false);
} finally {
  delete Object.prototype[1];
}
