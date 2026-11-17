// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "mapped arguments with a repeated parameter name"
flags: [noStrict]
---*/

function f(a, a) { arguments[0] = 3; arguments[1] = 4; return a; } assert.sameValue(String(f(1, 2)), "4");
