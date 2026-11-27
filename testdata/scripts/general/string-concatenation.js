// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string concatenation"
flags: [noStrict]
---*/

assert.sameValue(String("a" + 1 + 2 + "b" + (1 + 2)), "a12b3");

const packed = "abcd\u0000xyz";
const long = packed.concat("12345678", "!");
assert.sameValue(
	":".concat(...Array(18).fill(packed), long, ";"),
	":" + packed.repeat(19) + "12345678!;",
);
