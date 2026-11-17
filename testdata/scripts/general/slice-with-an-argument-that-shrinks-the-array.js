// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "slice with an argument that shrinks the array"
flags: [noStrict]
---*/

var a = [1, 2, 3, 4, 5, 6, 7, 8];
var r = a.slice({ valueOf() { a.length = 0; a.unshift(1); return 7; } }, { valueOf() { return 0; } });
assert.sameValue(String([r.length, a.join()].join("|")), "0|1");
