// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A product or a left shift with more than 2 ** 30 bits throws a RangeError"
flags: [noStrict]
---*/

var x = 1n << 2n ** 29n;
var results = [];
for (var f of [() => x * x, () => -x * x, () => 1n << 2n ** 30n, () => x << 2n ** 29n]) {
  try {
    f();
    results.push("no error");
  } catch (e) {
    results.push(e instanceof RangeError);
  }
}
assert.sameValue(results.join(), "true,true,true,true");
