// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "RegExp exec after converting lastIndex recompiles the RegExp"
flags: [noStrict]
---*/

var re = /a/;
re.lastIndex = { valueOf() { re.compile("b"); return 0; } };
assert.sameValue(String(re[Symbol.match]("b") !== null), "true");
