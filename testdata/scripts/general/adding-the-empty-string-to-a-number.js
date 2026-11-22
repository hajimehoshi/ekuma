// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "adding the empty string to a number gives the string of the number, and other operands keep their own conversions"
---*/

var numbers = [0, -0, 7, -5, 42, 12345678, 123456789, 0.1, 1.5e-7, 1e21, 2 ** 53, NaN, -Infinity];
var strings = "0,0,7,-5,42,12345678,123456789,0.1,1.5e-7,1e+21,9007199254740992,NaN,-Infinity";
var empties = ["", "abc".slice(3), String(""), [].join()];
for (var e of empties) {
  var after = [], before = [];
  for (var n of numbers) {
    after.push(n + e);
    before.push(e + n);
  }
  assert.sameValue(after.join(), strings);
  assert.sameValue(before.join(), strings);
}

var e = "";
var object = {
  valueOf() { return 1; },
  toString() { return "s"; },
};
assert.sameValue(object + e, "1");
assert.sameValue(e + 10n, "10");
assert.sameValue(3 + "a", "3a");
assert.sameValue("a" + 3, "a3");
assert.sameValue(true + e, "true");
assert.sameValue((12 + e) + "x", "12x");
