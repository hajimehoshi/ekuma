// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arrow function own keys"
flags: [noStrict]
---*/

var f = () => {}; f.x = 1; assert.sameValue(String(Object.getOwnPropertyNames(f).join()), "length,name,x");
