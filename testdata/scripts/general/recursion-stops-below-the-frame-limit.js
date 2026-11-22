// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "recursion stops below the engine's limit of 10000 frames"
---*/

var depth = 0;
function f() {
  depth++;
  f();
}

var caught;
try {
  f();
} catch (e) {
  caught = e;
}
assert.sameValue(caught instanceof RangeError, true);
assert.sameValue(depth < 10000, true, "depth " + depth);
