// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a stack trace cuts the long names that it shows"
flags: [noStrict]
---*/

var name = "x".repeat(1 << 20);
var o = {[Symbol.toStringTag]: name, f() { throw new Error(); }};
var s;
try { o.f(); } catch (e) { s = e.stack; }
assert.sameValue(s.length <= 1 << 12, true);
