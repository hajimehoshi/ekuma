// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "join with a separator that shrinks the array"
flags: [noStrict]
---*/

var a = ["a", "b", "c"];
assert.sameValue(a.join({ toString() { a.length = 1; return "-"; } }), "a--");
