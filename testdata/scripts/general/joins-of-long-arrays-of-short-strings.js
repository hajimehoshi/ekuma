// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "joins of arrays of a hundred strings, short ones alone and mixed with long ones, with separators up to and beyond eight bytes"
---*/

function concatenated(a, sep) {
  var s = "";
  for (var i = 0; i < a.length; i++) {
    if (i > 0) {
      s += sep;
    }
    s += a[i];
  }
  return s;
}

var short = [], mixed = [];
for (var i = 0; i < 100; i++) {
  short.push("x" + i);
  mixed.push(i % 7 == 3 ? "an element longer than eight bytes " + i : "y" + i);
}
for (var a of [short, mixed]) {
  for (var sep of ["", "-", "eight by", "a separator longer than eight bytes"]) {
    assert.sameValue(a.join(sep), concatenated(a, sep), JSON.stringify(sep));
  }
}

// A short element made at run time with a character outside ASCII.
assert.sameValue(["abcdef", "é" + 1].join("-").length, 9);
