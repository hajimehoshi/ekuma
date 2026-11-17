// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string search at surrogate boundaries"
flags: [noStrict]
---*/

var s = "😀x😀", h = "\ud83d", l = "\ude00";
assert.sameValue(String([s.indexOf(h), s.indexOf(l, 1), s.indexOf(h, 1), s.indexOf(l + "x" + h),
 s.lastIndexOf(h), s.lastIndexOf(l, 3), s.lastIndexOf(l + "x" + h),
 s.includes(h), s.indexOf("😀", 1), s.indexOf("x", 1), s.indexOf(h + "x")].join()), "0,1,3,1,3,1,1,true,3,2,-1");
