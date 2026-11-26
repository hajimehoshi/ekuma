// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "iteration over typed arrays on shared buffers"
---*/

var bigints = new BigUint64Array(new SharedArrayBuffer(16));
bigints.set([2n ** 64n - 1n, 1n]);
assert.sameValue([...bigints].join(), "18446744073709551615,1");

var bytes = new Uint8Array(new SharedArrayBuffer(8));
bytes.set([1, 2, 3, 4, 5, 6, 7, 8]);
assert.sameValue(JSON.stringify([...new Int8Array(bytes.buffer, 2, 3).entries()]), "[[0,3],[1,4],[2,5]]");

var growable = new SharedArrayBuffer(2, { maxByteLength: 4 });
var tracking = new Uint8Array(growable);
var visited = [];
var it = tracking.values();
for (var x of it) {
  visited.push(x);
  if (visited.length === 2) {
    growable.grow(3);
    tracking[2] = 9;
  }
}
growable.grow(4);
assert.sameValue(visited.join(), "0,0,9");
assert.sameValue(it.next().done, true);
