// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: A property added to an object with many properties hides a cached inherited one
---*/

function read(o) {
  return o.value;
}

const o = Object.create({ value: "inherited" });
// An object with this many properties keeps its layout to itself.
for (let i = 0; i < 200; i++) {
  o["p" + i] = i;
}
assert.sameValue(read(o), "inherited");
assert.sameValue(read(o), "inherited");
o.value = "own";
assert.sameValue(read(o), "own");
