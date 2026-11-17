// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string replacement callback code unit positions"
flags: [noStrict]
---*/

var positions = [];
"😀".replaceAll("", (match, pos) => { positions.push(pos); return ""; });
"😀😀".replaceAll("\ude00", (match, pos) => { positions.push(pos); return ""; });
assert.sameValue(String(positions.join()), "0,1,2,1,3");
