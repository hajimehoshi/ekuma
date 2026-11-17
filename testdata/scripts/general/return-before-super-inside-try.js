// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "return before super() inside try"
flags: [noStrict]
---*/

var log = [];
class D extends Object { constructor() { try { return; } catch (e) { log.push("caught"); } } }
try { new D(); } catch (e) { log.push(e.name); }
assert.sameValue(String(log.join()), "ReferenceError");
