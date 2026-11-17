// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "sparse array"
flags: [noStrict]
---*/

var a = []; a[5] = 1; a[100000] = 2; assert.sameValue(String([a.length, 3 in a, 5 in a, a[100000], Object.keys(a).join()].join("|")), "100001|false|true|2|5,100000");
