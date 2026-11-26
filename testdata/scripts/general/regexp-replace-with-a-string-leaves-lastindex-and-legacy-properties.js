// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "RegExp replace with a string leaves lastIndex and the legacy properties as its last exec does"
---*/

var global = /(\d)/g;
global.lastIndex = 3;
assert.sameValue("a1b2c3d".replace(global, "[$1]"), "a[1]b[2]c[3]d");
assert.sameValue(global.lastIndex, 0);
assert.sameValue(RegExp.$1, "3");
assert.sameValue(RegExp.lastMatch, "3");

var sticky = /\d/gy;
sticky.lastIndex = 1;
assert.sameValue("12a3".replace(sticky, "x"), "xxa3");
assert.sameValue(sticky.lastIndex, 0);

var once = /(\d)/;
once.lastIndex = 5;
assert.sameValue("a1b2".replace(once, "x"), "axb2");
assert.sameValue(once.lastIndex, 5);
assert.sameValue(RegExp.$1, "1");
