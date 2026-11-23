// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "compound assignment with number keys"
flags: [noStrict]
---*/

var a = [1, 2], o = {};
a[-0] += 5;
o[4294967295] = 1; o[4294967295] += 1; o[2 ** 32 - 1]++;
assert.sameValue([a.join(), o["4294967295"], Object.keys(o).join()].join("|"), "6,2|3|4294967295");
var caught;
try { var u; u[5000] += 1; } catch (e) { caught = e; }
assert.sameValue(caught instanceof TypeError, true);
