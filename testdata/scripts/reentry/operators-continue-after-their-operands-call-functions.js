// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a function continues with its variables after an operator converted an operand by calling a function"
---*/

var calls = 0;
function operand(x) {
  return {
    valueOf() {
      calls++;
      return x;
    },
    toString() {
      calls++;
      return x;
    },
  };
}

// Each function reads a variable that it set before the operator.
function check(f, left, right, want) {
  calls = 0;
  var got = f(left, right);
  assert.sameValue(got[0], want, "result of " + f);
  assert.sameValue(got[1], "local", "variable of " + f);
  assert.sameValue(calls > 0, true, "calls of " + f);
}

check(function (a) { var v = "local"; var r = a + 1; return [r, v]; }, operand(2), 0, 3);
check(function (a) { var v = "local"; var r = ~a; return [r, v]; }, operand(2), 0, -3);
check(function (a, b) { var v = "local"; var r = a - b; return [r, v]; }, operand(3), operand(1), 2);
check(function (a) { var v = "local"; var r = a - 1; return [r, v]; }, operand(3), 0, 2);
check(function (a, b) { var v = "local"; var r = a in b; return [r, v]; }, operand("k"), { k: 1 }, true);

check(function (a, b) { var v = "local"; var r = a < b; return [r, v]; }, operand(1), operand(2), true);
check(function (a, b) { var v = "local"; var r = a <= b; return [r, v]; }, operand(1), operand(2), true);
check(function (a, b) { var v = "local"; var r = a > b; return [r, v]; }, operand(1), operand(2), false);
check(function (a, b) { var v = "local"; var r = a >= b; return [r, v]; }, operand(1), operand(2), false);

check(function (a, b) { var v = "local"; if (a < b) { return [true, v]; } return [false, v]; }, operand(1), operand(2), true);
check(function (a, b) { var v = "local"; if (a <= b) { return [true, v]; } return [false, v]; }, operand(1), operand(2), true);
check(function (a, b) { var v = "local"; if (a > b) { return [true, v]; } return [false, v]; }, operand(1), operand(2), false);
check(function (a, b) { var v = "local"; if (a >= b) { return [true, v]; } return [false, v]; }, operand(1), operand(2), false);

check(function (a) { var v = "local"; if (a < 2) { return [true, v]; } return [false, v]; }, operand(1), 0, true);
check(function (a) { var v = "local"; if (a <= 2) { return [true, v]; } return [false, v]; }, operand(1), 0, true);
check(function (a) { var v = "local"; if (a > 2) { return [true, v]; } return [false, v]; }, operand(1), 0, false);
check(function (a) { var v = "local"; if (a >= 2) { return [true, v]; } return [false, v]; }, operand(1), 0, false);

// The superclass of a class can run code when its prototype is read.
check(function (a) {
  var v = "local";
  class C extends a {}
  return [Object.getPrototypeOf(C.prototype) === Object.prototype, v];
}, new Proxy(function () {}, {
  get(target, key) {
    calls++;
    return key === "prototype" ? Object.prototype : target[key];
  },
}), 0, true);

// The first call of a setter that only returns a property fills the cache of its read.
class P {
  y = "y";
  set #x(v) { return this.y; }
  store(value) {
    var v = "local";
    this.#x = value;
    return v;
  }
}
var p = new P();
for (var i = 0; i < 3; i++) {
  assert.sameValue(p.store(i), "local");
}
