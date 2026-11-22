// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a relational loop condition converts its left operand, then its right one"
---*/

var log = [];
function value(name, x) {
  return {
    valueOf() {
      log.push(name);
      return x;
    },
  };
}

var k = 0;
while (value("a", k) < value("b", 1)) k++;
k = 0;
while (value("c", k) <= value("d", 0)) k++;
k = 1;
while (value("e", k) > value("f", 0)) k--;
k = 0;
while (value("g", k) >= value("h", 0)) k--;
k = 0;
while (value("i", k) < 1) k++;
assert.sameValue(log.join(""), "ababcdcdefefghghii");
