// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "walk of a sparse array with many indices sees indices added by the callback"
flags: [noStrict]
---*/

var a = []; for (var i = 0; i < 200; i++) a[i * 1000 + 500] = i; var seen = [];
a.forEach(function (v, i) { if (i === 500) { a.x = 0; a[1700] = -1; a[300] = -2; } if (i < 3000) seen.push(i); });
assert.sameValue(String(seen.join()), "500,1500,1700,2500");
