// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string as UTF-16"
flags: [noStrict]
---*/

var s = "a\u{1F600}b"; assert.sameValue(String([s.length, s.charCodeAt(1), s.charCodeAt(2), s[3], [...s].length, s.slice(1, 2).length].join()), "4,55357,56832,b,3,1");
