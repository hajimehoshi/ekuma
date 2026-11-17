// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "number formatting"
flags: [noStrict]
---*/

assert.sameValue(String([0.1 + 0.2, 1e21, 1e-7, -0, 123456789012345680000, (255).toString(16), (1.005).toFixed(2), 5e-324].join("|")), "0.30000000000000004|1e+21|1e-7|0|123456789012345680000|ff|1.00|5e-324");
