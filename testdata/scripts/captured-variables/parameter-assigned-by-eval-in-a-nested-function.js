// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parameter assigned by eval in a nested function"
flags: [noStrict]
---*/

function f(a) { const g = () => a; (() => eval("a = 5"))(); return g(); } assert.sameValue(String(f(1)), "5");
