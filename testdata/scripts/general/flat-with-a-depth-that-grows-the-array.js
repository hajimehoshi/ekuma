// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "flat with a depth that grows the array"
flags: [noStrict]
---*/

var a = [1, [2]]; assert.sameValue(String(a.flat({ valueOf() { a.push(3); return 1; } }).join()), "1,2");
