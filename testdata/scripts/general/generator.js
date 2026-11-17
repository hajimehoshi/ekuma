// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "generator"
flags: [noStrict]
---*/

function* g(n) { for (var i = 0; i < n; i++) { var x = yield i; if (x) i += x; } return "done"; }
var it = g(5), out = []; out.push(it.next().value); out.push(it.next(2).value); out.push(it.next().value);
out.push(it.next().value); assert.sameValue(String(out.join() + [...g(3)].join("")), "0,3,4,done012");
