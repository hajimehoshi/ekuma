// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "try finally with return and break"
flags: [noStrict]
---*/

var log = [];
function f() { try { return "r"; } finally { log.push("f1"); } }
out: for (var i = 0; i < 3; i++) { try { if (i == 1) break out; } finally { log.push("i" + i); } }
assert.sameValue(String(f() + ":" + log.join()), "r:i0,i1,f1");
