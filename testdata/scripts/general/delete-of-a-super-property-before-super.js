// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "delete of a super property before super()"
flags: [noStrict]
---*/

var log = [];
class D extends Object { constructor() { delete super[log.push("key")]; } }
try { new D(); } catch (e) { log.push(e.name); }
assert.sameValue(String(log.join()), "ReferenceError");
