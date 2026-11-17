// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "appended string as a property key and in comparisons"
flags: [noStrict]
---*/

var k = "";
for (var i = 0; i < 40; i++) k += "k";
var o = {};
o[k] = 1;
var k2 = k + "!";
o[k2] = 2;
var m = new Map([[k, "m"]]);
var lit = "k".repeat(40);
assert.sameValue(String([o[lit], o[lit + "!"], m.get(lit), k === lit, k < k2, Object.keys(o).join().length].join(" ")), "1 2 m true true 82");
