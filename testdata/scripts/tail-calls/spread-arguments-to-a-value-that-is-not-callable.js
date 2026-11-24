// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "spread arguments to a value that is not callable"
flags: [onlyStrict]
---*/

function f(g, a) { return g(...a); }
let message;
try {
  f(1, [2]);
} catch (e) {
  message = e.message;
}
assert.sameValue(message, "1 is not a function");
