// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "bitwise and shift operators with a literal outside the int32 range"
flags: [noStrict]
---*/

var h = -123456789, v = 5, w = 7;
w |= 0x80000000;
assert.sameValue(String([h & 0xFFFFFFFF, h | 0x80000000, h << 1e21, h >> 33, h >>> 32, v | 1e21, v & 1e21,
 0 | 4294967301, v | 1e400, w].join()), "-123456789,-123456789,-123456789,-61728395,4171510507,-559939579,0,5,5,-2147483641");
