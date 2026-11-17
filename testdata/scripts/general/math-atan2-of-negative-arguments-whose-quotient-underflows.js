// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Math.atan2 of negative arguments whose quotient underflows"
flags: [noStrict]
---*/

assert.sameValue(String([Math.atan2(-5e-324, -2.5), Math.atan2(-1e-200, -1e200)].join()), "-3.141592653589793,-3.141592653589793");
