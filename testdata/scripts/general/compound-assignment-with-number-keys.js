// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "compound assignment with number keys"
flags: [noStrict]
---*/

var a = [1, 2], o = {}, r;
a[-0] += 5;
o[4294967295] = 1; o[4294967295] += 1; o[2 ** 32 - 1]++;
try { var u; u[5000] += 1; } catch (e) { r = e.message; }
assert.sameValue(String([a.join(), o["4294967295"], Object.keys(o).join(), r].join("|")), "6,2|3|4294967295|cannot read properties of undefined (reading '5000')");
