// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "joins of arrays of strings, and of the arrays and separators that need the full algorithm"
---*/

var short = ["a", "bc", "def"];
var long = ["a string longer than eight bytes", "another one"];
assert.sameValue([short.join(""), short.join(), short.join(" | "), long.join("+"), [].join("x"),
  ["only"].join("x")].join("/"), "abcdef/a,bc,def/a | bc | def/a string longer than eight bytes+another one//only");

// Joins longer than eight bytes with characters outside ASCII.
var r = ["abcdef", "ghijkl"].join("é"), u = ["ébcdef", "ghijkl"].join(",");
assert.sameValue([r.length, r.charCodeAt(6), u.length, u[12]].join(), "13,233,13,l");

// Arrays with holes, a length past their elements, and elements of other types.
var holes = ["a", , "c"], longer = ["a"];
longer.length = 1000;
assert.sameValue([holes.join("-"), longer.join("-").length, ["a", 1, null, "b"].join("-")].join("/"), "a--c/1000/a-1--b");

// A join of an array that a join of the same array reaches is empty, even of strings only.
var cyc = ["a"];
cyc.push({
  toString() {
    cyc[1] = "b";
    return cyc.join("+");
  },
});
assert.sameValue(cyc.join("-"), "a-");
