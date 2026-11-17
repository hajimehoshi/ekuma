// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "fill with a start that shrinks the array"
flags: [noStrict]
---*/

var a = [1, 2, 3, 4]; a.fill(9, { valueOf() { a.length = 1; return 1; } }); assert.sameValue(String(a.join()), "1,9,9,9");
