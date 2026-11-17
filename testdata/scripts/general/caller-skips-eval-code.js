// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "caller skips eval code"
flags: [noStrict]
---*/

function f() { return f.caller; }
function g() { return eval("f()"); }
function h() { return eval("eval('f()')"); }
assert.sameValue(String([g() === g, h() === h].join()), "true,true");
