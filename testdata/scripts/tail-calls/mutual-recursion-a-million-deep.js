// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "mutual recursion a million deep"
flags: [onlyStrict]
---*/

function even(n) { return n === 0 ? true : odd(n - 1); }
function odd(n) { return n === 0 ? false : even(n - 1); }
assert.sameValue(String(even(1e6)), "true");
