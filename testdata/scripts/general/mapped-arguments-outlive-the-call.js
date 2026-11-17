// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "mapped arguments outlive the call"
flags: [noStrict]
---*/

function f(a) { return [arguments, function() { return a; }, function(v) { a = v; }]; }
var r = f(1); r[0][0] = 2; var x = r[1](); r[2](3);
function* g(a) { yield arguments; yield a; }
var it = g(1); it.next().value[0] = 4;
assert.sameValue(String(x + "," + r[0][0] + "," + it.next().value), "2,3,4");
