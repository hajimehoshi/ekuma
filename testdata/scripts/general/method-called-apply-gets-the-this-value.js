// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "method called apply gets the this value"
flags: [noStrict]
---*/

var t = {};
var o = { apply(thisValue, args) { return thisValue === t && args[0] === 1; } };
function f(a) { return o.apply(this, arguments); }
assert.sameValue(f.call(t, 1), true);
