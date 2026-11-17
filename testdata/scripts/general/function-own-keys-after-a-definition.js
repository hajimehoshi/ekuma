// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "function own keys after a definition"
flags: [noStrict]
---*/

function f() {} Object.defineProperty(f, "x", { value: 1 }); assert.sameValue(String(Object.getOwnPropertyNames(f).join()), "length,name,arguments,caller,prototype,x");
