// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "switch with fallthrough"
flags: [noStrict]
---*/

function f(x) { var r = ""; switch (x) { case 1: r += "a"; case 2: r += "b"; break; default: r += "d"; } return r; } assert.sameValue(String(f(1) + f(2) + f(3)), "abbd");
