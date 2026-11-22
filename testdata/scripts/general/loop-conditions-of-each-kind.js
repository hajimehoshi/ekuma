// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "loops run as many iterations as their conditions of each kind allow"
---*/

function count(loop) {
  var n = 0;
  loop(function () {
    n++;
  });
  return n;
}

var n = 3, z = 0;
assert.sameValue([
  count(it => { for (var i = 0; i < n; i++) it(); }),
  count(it => { for (var i = 0; i <= n; i++) it(); }),
  count(it => { for (var i = n; i > z; i--) it(); }),
  count(it => { for (var i = n; i >= z; i--) it(); }),
  count(it => { for (var i = 0; i < 3; i++) it(); }),
  count(it => { for (var i = 0; i <= 3; i++) it(); }),
  count(it => { for (var i = 3; i > 0; i--) it(); }),
  count(it => { for (var i = 3; i >= 0; i--) it(); }),
].join(), "3,4,3,4,3,4,3,4");

// NaN fails the comparisons, and the other types take the full comparison.
assert.sameValue([
  count(it => { for (var i = NaN; i < n; i++) it(); }),
  count(it => { var x = NaN; while (x >= 0) it(); }),
  count(it => { for (var s = "a"; s < "aaaa"; s += "a") it(); }),
  count(it => { for (var b = 0n; b < 3; b++) it(); }),
  count(it => { for (var b = 0n; b <= 3n; b++) it(); }),
  count(it => { for (var b = 3n; b > 0; b--) it(); }),
].join(), "0,0,3,3,4,3");

// Conditions converted with ToBoolean.
assert.sameValue([
  count(it => { var k = 3; while (k) { it(); k--; } }),
  count(it => { var s = "abc"; while (s) { it(); s = s.slice(1); } }),
  count(it => { var o = { next: { next: { next: null } } }; while (o) { it(); o = o.next; } }),
  count(it => { var u; do it(); while (u); }),
  count(it => { while (false) it(); }),
  count(it => { var k = 0; while (true) { if (k++ == 3) break; it(); } }),
].join(), "3,3,3,1,0,3");

// Compound, negated and equality conditions, do-while, and continue.
assert.sameValue([
  count(it => { for (var i = 0, j = 5; i < 3 && j > 0; i++, j -= 2) it(); }),
  count(it => { for (var i = 0, j = 0; i < 2 || j < 3; i++, j++) it(); }),
  count(it => { for (var i = 0; i !== 3; i++) it(); }),
  count(it => { var k = 0; while (!(k >= 3)) { it(); k++; } }),
  count(it => { var k = 0; do { it(); k++; } while (k < 3); }),
  count(it => { var k = 5; do { it(); k++; } while (k < 3); }),
  count(it => { for (var i = 0; i < 5; i++) { if (i % 2) continue; it(); } }),
  count(it => { outer: for (var i = 0; i < 3; i++) for (var j = 0; j < 3; j++) { if (j == 1) continue outer; it(); } }),
].join(), "3,3,3,3,3,1,3,3");

// Comparisons with undefined, null and booleans, and an IsHTMLDDA object, which is loosely null.
var list = { next: { next: { next: null } } }, dda = $262.IsHTMLDDA;
assert.sameValue([
  count(it => { var o = list; while (o !== null) { it(); o = o.next; } }),
  count(it => { var o = list; while (o != null) { it(); o = o.next; } }),
  count(it => { var o = list; while (!(o === null)) { it(); o = o.next; } }),
  count(it => { var u; while (u === undefined) { it(); u = 0; } }),
  count(it => { var u; while (u == null) { it(); u = 0; } }),
  count(it => { var b = true; do { it(); b = !b; } while (b === false); }),
  count(it => { var o = dda, k = 0; while (o == null) { it(); if (++k == 2) o = 0; } }),
  count(it => { var o = 1, k = 0; while (o != null) { it(); if (++k == 2) o = dda; else if (k > 3) break; } }),
  count(it => { var k = 1; while (k == true) { it(); k = 2; } }),
].join(), "3,3,3,1,1,2,2,2,1");
