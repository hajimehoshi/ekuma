// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "function own keys after an assignment"
flags: [noStrict]
---*/

function f() {} f.x = 1; assert.sameValue(String(Object.getOwnPropertyNames(f).join()), "length,name,arguments,caller,prototype,x");
