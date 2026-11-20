// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: Reading a field before returning an argument preserves the return value
---*/

const o = { x: 1, read(v) { this.x; return v; } };
o.read(2);
assert.sameValue(o.read(3), 3);
