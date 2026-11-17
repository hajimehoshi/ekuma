// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "function own keys after deleting an initial property"
flags: [noStrict]
---*/

function f() {} delete f.name; f.x = 1; assert.sameValue(String(Object.getOwnPropertyNames(f).join()), "length,arguments,caller,prototype,x");
