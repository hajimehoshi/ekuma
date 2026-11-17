// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "remainder of negative zero by a register"
flags: [noStrict]
---*/

var x = -0, y = 1; assert.sameValue(String(1 / (x % y)), "-Infinity");
