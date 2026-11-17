// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "optional chaining and nullish"
flags: [noStrict]
---*/

var o = { a: { b: 0 } }; assert.sameValue(String([o?.a?.b ?? "x", o.z?.b.c ?? "y", o.f?.()].join()), "0,y,");
