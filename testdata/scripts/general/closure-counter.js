// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "closure counter"
flags: [noStrict]
---*/

function counter() { var n = 0; return function() { n++; return n; }; }
var c = counter(); c(); c(); assert.sameValue(String(c()), "3");
