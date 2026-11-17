// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "enumerable property"
flags: [noStrict]
---*/

Temporal.x = 1; assert.sameValue(String(Object.keys(Temporal).join() + " " + Object.getOwnPropertyNames(Temporal).indexOf("x")), "x 9");
