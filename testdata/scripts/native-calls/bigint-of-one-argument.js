// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt called with one argument of each kind"
---*/

assert.sameValue([BigInt(5), BigInt(-0), BigInt(-(2 ** 63)), BigInt(2 ** 63), BigInt(2 ** 64), BigInt("12"),
  BigInt(true), BigInt(10n)].join(), "5,0,-9223372036854775808,9223372036854775808,18446744073709551616,12,1,10");
var errors = [];
for (var x of [1.5, NaN, Infinity, -Infinity]) {
  try {
    BigInt(x);
  } catch (e) {
    errors.push(e.constructor.name);
  }
}
assert.sameValue(errors.join(), "RangeError,RangeError,RangeError,RangeError");
try {
  new BigInt(1);
} catch (e) {
  errors.push(e.constructor.name);
}
assert.sameValue(errors.length, 5);
assert.sameValue(errors[4], "TypeError");

// A conversion that runs code runs inside BigInt.
var line;
try {
  BigInt({ valueOf() { throw new Error("m"); } });
} catch (e) {
  line = e.stack.split("\n")[2];
}
assert.sameValue(line, "    at BigInt (<anonymous>)");
