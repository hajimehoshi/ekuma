// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "apply that is not Function.prototype.apply"
flags: [onlyStrict]
---*/

var o = { apply(t, args) { return args[0] === 0 ? args.length : f(args[0] - 1, 2); } };
function f(n) { return o.apply(null, arguments); }
assert.sameValue(String(f(1e6)), "2");
