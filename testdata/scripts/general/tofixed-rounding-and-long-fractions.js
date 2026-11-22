// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "toFixed rounding and long fractions"
flags: [noStrict]
---*/

assert.sameValue([(2.5).toFixed(0), (-1.375).toFixed(2), (255.0625).toFixed(3), (9.99).toFixed(1),
	(-0.0001).toFixed(2)].join(" "), "3 -1.38 255.063 10.0 -0.00");
assert.sameValue([(0.001).toFixed(5), (123456789012.1).toFixed(15), (12345678.1).toFixed(19),
	(2 ** 63 + 2 ** 11).toFixed(2)].join(" "),
	"0.00100 123456789012.100006103515625 12345678.0999999996274709702 9223372036854777856.00");
