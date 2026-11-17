// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parameter assigned in a with statement"
flags: [noStrict]
---*/

function f(a) { const g = () => a; with ({}) { a = 6; } return g(); } assert.sameValue(String(f(1)), "6");
