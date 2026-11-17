// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parameter declared again by var"
flags: [noStrict]
---*/

function f(a) { const g = () => a; for (var a of [8]); return g(); } assert.sameValue(String(f(1)), "8");
