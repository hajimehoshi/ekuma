// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "spread and rest"
flags: [noStrict]
---*/

function f(a, ...r) { return a + r.length; } assert.sameValue(String(f(...[1, 2, 3], ...[4])), "4");
