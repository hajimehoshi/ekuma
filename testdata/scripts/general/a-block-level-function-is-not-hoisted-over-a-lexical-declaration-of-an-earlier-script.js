// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a block-level function of sloppy code gets no global variable over a lexical declaration of an earlier script"
flags: [noStrict, noRepeat]
noRepeatReason: "a script of $262.evalScript needs the global lexical declaration"
---*/

let f = 1;
assert.sameValue($262.evalScript("{ function f() {} } f"), 1);
