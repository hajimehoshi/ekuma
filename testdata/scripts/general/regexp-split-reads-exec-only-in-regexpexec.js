// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "RegExp split reads exec only in RegExpExec"
flags: [noStrict]
---*/

var gets = 0;
class R extends RegExp { get exec() { gets++; return RegExp.prototype.exec; } }
assert.sameValue(String("abc".split(new R("b")).join() + ":" + gets), "a,c:3");
