// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "toPrecision fixed rounding and exponent carry"
flags: [noStrict]
---*/

assert.sameValue(String([(1.25).toPrecision(2), (-9.95).toPrecision(2), (-9.999).toPrecision(3), (99.5).toPrecision(2),
(0.0000009999999).toPrecision(2)].join(" ")), "1.3 -9.9 -10.0 1.0e+2 0.0000010");
