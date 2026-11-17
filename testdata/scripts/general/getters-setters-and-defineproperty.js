// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "getters, setters and defineProperty"
flags: [noStrict]
---*/

var o = { _v: 1, get v() { return this._v; }, set v(x) { this._v = x * 2; } };
o.v = 5; Object.defineProperty(o, "w", { value: 3, enumerable: false });
assert.sameValue(String(o.v + ":" + o.w + ":" + Object.keys(o).join()), "10:3:_v,v");
