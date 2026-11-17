// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Math.log2 just above 1"
flags: [noStrict]
---*/

assert.sameValue(String(Math.log2(1.0000000000000002)), "3.203426503814917e-16");
