// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parameter assigned by eval"
flags: [noStrict]
---*/

function f(a) { const g = () => a; eval("a = 4"); return g(); } assert.sameValue(String(f(1)), "4");
