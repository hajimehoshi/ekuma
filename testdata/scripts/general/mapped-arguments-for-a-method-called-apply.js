// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "mapped arguments for a method called apply"
flags: [noStrict]
---*/

var o = { apply(t, args) { args[0] = 2; return args.callee === f; } };
function f(a) { return o.apply(this, arguments) + "," + a; } assert.sameValue(String(f(1)), "true,2");
