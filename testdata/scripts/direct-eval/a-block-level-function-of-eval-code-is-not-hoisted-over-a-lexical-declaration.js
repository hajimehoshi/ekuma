// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a block-level function of eval code gets no variable over a lexical declaration of the script"
flags: [noStrict]
---*/

let h = 1;
eval("{ function h() {} }");
assert.sameValue(h, 1);
