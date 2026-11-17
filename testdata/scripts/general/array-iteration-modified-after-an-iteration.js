// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "array iteration modified after an iteration"
flags: [noStrict]
---*/

[...[1, 2]];
Array.prototype[Symbol.iterator] = function* () { yield "X"; };
assert.sameValue(String(String([...[1, 2]])), "X");
