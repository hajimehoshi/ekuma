// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "toExponential rounding and exponents"
flags: [noStrict]
---*/

assert.sameValue([(123.456).toExponential(2), (0.000123).toExponential(1), (5).toExponential(0),
	(12345678901234).toExponential(3), (-0.00625).toExponential(1), (3.1).toExponential(18)].join(" "),
	"1.23e+2 1.2e-4 5e+0 1.235e+13 -6.3e-3 3.100000000000000089e+0");
