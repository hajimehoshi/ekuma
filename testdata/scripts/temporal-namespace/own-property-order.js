// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "own property order"
flags: [noStrict]
---*/

Object.defineProperty(Temporal, "x", { value: 1 }); assert.sameValue(String(Object.getOwnPropertyNames(Temporal).slice(-2).join()), "Now,x");
