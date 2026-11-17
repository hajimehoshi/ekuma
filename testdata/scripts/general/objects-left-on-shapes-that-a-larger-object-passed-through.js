// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "objects left on shapes that a larger object passed through"
flags: [noStrict]
---*/

function make(n) { var o = {}; for (var i = 0; i < n; i++) o["q" + i] = i; return o; }
var a = make(12), b = make(11), c = make(10);
assert.sameValue(String([a.q11, b.q10, b.q11, c.q9, c.q10, c.q3, a.q0].map(String).join()), "11,10,undefined,9,undefined,3,0");
