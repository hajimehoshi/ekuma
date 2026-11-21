// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "nondecimal number strings use the shortest significand that rounds to the input"
---*/

assert.sameValue((1.2345e27).toString(5), "314404114120102" + "0".repeat(24));

// The rounding interval below 0.5 is half as wide, so the shortest base-3 value lies above 0.5.
assert.sameValue((0.5).toString(3), "0." + "1".repeat(33) + "2");

// Two one-digit significands round to this subnormal. The closer one is b.
assert.sameValue(Number.MIN_VALUE.toString(14), "0." + "0".repeat(282) + "b");
