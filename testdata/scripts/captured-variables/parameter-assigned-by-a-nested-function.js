// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parameter assigned by a nested function"
flags: [noStrict]
---*/

function f(a) { const g = () => () => a; (() => { a = 3; })(); return g()(); } assert.sameValue(String(f(1)), "3");
