// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "slice reads an inherited element in the requested range"
---*/

var a = [0, 1, , 3];
Array.prototype[2] = 42;
assert.sameValue(a.slice(1, 3)[1], 42);
