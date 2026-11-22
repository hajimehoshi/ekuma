// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Math.min and Math.max of two arguments, numbers or not, and other functions called the same way"
---*/

function show(x) {
  return Object.is(x, -0) ? "-0" : String(x);
}

var pairs = [[1, 2], [2, 1], [0, -0], [-0, 0], [NaN, 1], [1, NaN], [-Infinity, Infinity], [1.5, -2.5]];
var results = [];
for (var pair of pairs) {
  results.push(show(Math.min(pair[0], pair[1])) + "/" + show(Math.max(pair[0], pair[1])));
}
assert.sameValue(results.join(), "1/2,1/2,-0/0,-0/0,NaN/NaN,NaN/NaN,-Infinity/Infinity,-2.5/1.5");

var log = [];
var three = {
  valueOf() { log.push("three"); return 3; },
};
var four = {
  valueOf() { log.push("four"); return 4; },
};
assert.sameValue(Math.min(three, four), 3);
assert.sameValue(log.join(), "three,four");
assert.sameValue(Math.max("7", 5), 7);
assert.sameValue(Math.min(4, 2, 3), 2);
assert.sameValue(Math.max.call(null, 8, 6), 8);
assert.sameValue(Math.pow(2, 3), 8);

var original = Math.min;
Math.min = function (a, b) {
  return "replaced";
};
assert.sameValue(Math.min(1, 2), "replaced");
Math.min = original;
