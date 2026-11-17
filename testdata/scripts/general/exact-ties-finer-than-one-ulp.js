// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "exact ties finer than one ulp"
flags: [noStrict]
---*/

assert.sameValue(String([(2 ** -24).toExponential(15), (0.45).toFixed(53), (2 ** -24).toPrecision(16),
(-9.5).toExponential(0), (99.5).toFixed(0)].join(" ")), "5.960464477539063e-8 0.45000000000000001110223024625156540423631668090820313 5.960464477539063e-8 -1e+1 100");
