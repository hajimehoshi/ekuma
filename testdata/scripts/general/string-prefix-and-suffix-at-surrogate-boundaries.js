// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string prefix and suffix at surrogate boundaries"
flags: [noStrict]
---*/

var s = "😀", h = "\ud83d", l = "\ude00";
assert.sameValue(String([s.startsWith(h), s.startsWith(l, 1), s.endsWith(l), s.endsWith(h, 1),
 s.startsWith(s, 1), s.endsWith(s, 1)].join()), "true,true,true,true,false,false");
