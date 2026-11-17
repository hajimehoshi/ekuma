// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "generator suspended across an assignment"
flags: [noStrict]
---*/

function* gen(a) { let x = a; const g = () => x + a; yield g(); x = 10; yield g(); }
assert.sameValue(String([...gen(1)].join()), "2,11");
