// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "array methods"
flags: [noStrict]
---*/

var a = [5, 1, 4]; a.push(3); a.sort(); a.splice(1, 1, "x", "y");
assert.sameValue(String([a.join(), a.indexOf("y"), a.slice(-2).join(), a.reduce((s, v) => s + v, ""), a.length].join("|")), "1,x,y,4,5|2|4,5|1xy45|5");
