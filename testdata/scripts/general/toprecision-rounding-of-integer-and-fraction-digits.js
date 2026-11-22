// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "toPrecision rounding of integer and fraction digits"
flags: [noStrict]
---*/

assert.sameValue([(125).toPrecision(2), (12.5).toPrecision(2), (12.25).toPrecision(3), (0.125).toPrecision(2),
	(2 ** 64 - 2 ** 11).toPrecision(18)].join(" "), "1.3e+2 13 12.3 0.13 1.84467440737095496e+19");
assert.sameValue([(0.000123).toPrecision(2), (0.1171875).toPrecision(3), (0.0999).toPrecision(2)].join(" "),
	"0.00012 0.117 0.10");
assert.sameValue([(2 ** -23).toPrecision(3), (2 ** -17).toPrecision(3), (123456).toPrecision(2),
	(12).toPrecision(2)].join(" "), "1.19e-7 0.00000763 1.2e+5 12");
