// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parameter assigned after the closure is created"
flags: [noStrict]
---*/

function f(a) { const g = () => a; a = 2; return g(); } assert.sameValue(String(f(1)), "2");
