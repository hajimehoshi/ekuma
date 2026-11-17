// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "generator return runs finally"
flags: [noStrict]
---*/

var log = []; function* g() { try { yield 1; yield 2; } finally { log.push("cleanup"); } }
for (var v of g()) { log.push(v); break; } assert.sameValue(String(log.join()), "1,cleanup");
