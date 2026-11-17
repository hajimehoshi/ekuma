// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Math functions near the ends of the double range"
flags: [noStrict]
---*/

assert.sameValue(String([Math.cosh(710), Math.sinh(-710), Math.cosh(711), Math.expm1(709.782712893384),
	Math.exp(-745), Math.cosh(709.5), Math.sinh(-709.5)].join()), "1.1169973830808557e+308,-1.1169973830808557e+308,Infinity,1.7976931348622732e+308,5e-324,6.774931596573164e+307,-6.774931596573164e+307");
