// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "let per iteration and closures"
flags: [noStrict]
---*/

var fs = []; for (let i = 0; i < 3; i++) fs.push(() => i); assert.sameValue(String(fs.map(f => f()).join()), "0,1,2");
