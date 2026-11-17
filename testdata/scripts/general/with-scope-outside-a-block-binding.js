// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "with scope outside a block binding"
flags: [noStrict]
---*/

var o = {x: 1, y: 2, this: 3}; var r = [];
with (o) { let x = 5; { let y = 6; r.push(x, y, this === globalThis); } }
assert.sameValue(String(r.join()), "5,6,true");
