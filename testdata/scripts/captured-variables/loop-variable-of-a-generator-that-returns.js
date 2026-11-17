// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "loop variable of a generator that returns"
flags: [noStrict]
---*/

var f;
function* gen() { for (let v of [1, 2]) { f = () => v; if (v == 5) v = "x"; yield v; } }
var it = gen(); it.next(); it.return("r");
assert.sameValue(String(f()), "1");
