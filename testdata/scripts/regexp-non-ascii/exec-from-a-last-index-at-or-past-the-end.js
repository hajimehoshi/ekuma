// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a global or sticky exec fails from a lastIndex past the end and matches at the end"
---*/

var r = /(?:)/g;
r.lastIndex = 1;
assert.sameValue(r.exec(""), null);
assert.sameValue(r.lastIndex, 0);

var y = /ᵒ/y;
y.lastIndex = 50;
assert.sameValue(y.exec("ᶠᵒˣ"), null);
assert.sameValue(y.lastIndex, 0);

var e = /$/g;
var u = "ᶠᵒˣ jumped";
e.lastIndex = u.length;
var m = e.exec(u);
assert.sameValue(m[0], "");
assert.sameValue(m.index, u.length);
