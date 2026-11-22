// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "stores at the length of an array that has room for more elements call an index setter of Object.prototype"
---*/

// five returns an array of five elements with room for more.
function five() {
  var a = [];
  for (var i = 0; i < 5; i++) {
    a[i] = i;
  }
  return a;
}

var log = [];
var a = five();
Object.defineProperty(Object.prototype, 5, {
  set(v) { log.push(v); },
  configurable: true,
});
a[5] = "stored";
assert.sameValue([a.length, a.hasOwnProperty(5), log.join()].join(), "5,false,stored");
