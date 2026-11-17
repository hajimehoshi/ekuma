// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "call a million deep"
flags: [onlyStrict]
---*/

function f(n, acc) { return n === 0 ? this.k + acc : f.call(this, n - 1, acc + 1); }
assert.sameValue(String(f.call({k: 1}, 1e6, 0)), "1000001");
