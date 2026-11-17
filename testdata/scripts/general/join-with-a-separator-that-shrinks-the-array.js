// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "join with a separator that shrinks the array"
flags: [noStrict]
---*/

var a = [1, 2, 3]; assert.sameValue(String(a.join({ toString() { a.length = 1; return "-"; } })), "1--");
