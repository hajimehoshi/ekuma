// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "per-iteration bindings"
flags: [noStrict]
---*/

var fs = [];
for (const x of [1, 2]) { const y = x * 10; fs.push(() => x + y); }
for (let i = 0, k = 100; i < 2; i++) fs.push(() => i + k);
assert.sameValue(String(fs.map(f => f()).join()), "11,22,100,101");
