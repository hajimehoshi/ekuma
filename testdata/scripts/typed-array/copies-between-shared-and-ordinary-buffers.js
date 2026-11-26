// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "copies of elements between shared and ordinary buffers"
---*/

function filled(buffer) {
  new Uint8Array(buffer).set([1, 2, 3, 4, 5, 6, 7, 8]);
  return buffer;
}

// sliceInto copies the bytes from 2 to 4 of src to the bytes from 3 to 5 of dst with slice.
function sliceInto(src, dst) {
  var view = new Uint8Array(src, 1);
  view.constructor = {
    [Symbol.species]: function () { return new Uint8Array(dst, 3); }
  };
  view.slice(1, 4);
  return new Uint8Array(dst).join();
}

var ordinary = filled(new ArrayBuffer(8));
var shared = filled(new SharedArrayBuffer(8));

assert.sameValue(sliceInto(shared, new ArrayBuffer(8)), "0,0,0,3,4,5,0,0");
assert.sameValue(sliceInto(ordinary, new SharedArrayBuffer(8)), "0,0,0,3,4,5,0,0");
assert.sameValue(sliceInto(shared, new SharedArrayBuffer(8)), "0,0,0,3,4,5,0,0");

var target = new Uint8Array(8);
target.subarray(1).set(new Uint8Array(shared, 2, 3), 2);
assert.sameValue(target.join(), "0,0,0,3,4,5,0,0");
assert.sameValue(new Uint8Array(new Uint8Array(shared, 2, 3)).join(), "3,4,5");

// Elements of more than one byte, and elements that are converted.
var wide = new Uint16Array(shared, 2, 2);
assert.sameValue(new Uint16Array(wide).join(), "1027,1541");
assert.sameValue(new Uint8Array(wide).join(), "3,5");
var wideTarget = new Uint16Array(4);
wideTarget.set(wide, 1);
assert.sameValue(wideTarget.join(), "0,1027,1541,0");
target.fill(0);
target.set(wide, 1);
assert.sameValue(target.join(), "0,3,5,0,0,0,0,0");
