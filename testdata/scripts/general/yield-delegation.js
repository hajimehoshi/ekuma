// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "yield delegation"
flags: [noStrict]
---*/

function* a() { yield 1; return 2; } function* b() { var r = yield* a(); yield r; } assert.sameValue(String([...b()].join()), "1,2");
