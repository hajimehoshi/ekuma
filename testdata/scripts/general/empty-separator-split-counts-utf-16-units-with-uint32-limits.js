// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "empty separator split counts UTF-16 units with uint32 limits"
flags: [noStrict]
---*/

var s = "😀x";
assert.sameValue(String([s.split("", 4294967297).map(c => c.charCodeAt(0)).join(),
 s.split("", -1).map(c => c.charCodeAt(0)).join()].join(";")), "55357;55357,56832,120");
