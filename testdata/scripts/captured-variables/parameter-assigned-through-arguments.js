// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parameter assigned through arguments"
flags: [noStrict]
---*/

function f(a) { const g = () => a; (() => { arguments[0] = 7; })(); return g(); } assert.sameValue(String(f(1)), "7");
