// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a closure created in the condition of a for loop sees the bindings of its iteration"
---*/

var fs = [];
for (let i = 0; fs.push(() => i), i < 3; i++) {}
assert.sameValue(fs.map(f => f()).join(), "0,1,2,3");
