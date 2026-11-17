// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "for-of reports a throwing return method when a generator returns"
flags: [noStrict]
---*/

var it = { [Symbol.iterator]() { return { next() { return { done: false }; }, return() { throw "closed"; } }; } };
function* g() { for (var x of it) yield 1; }
var r = g(); r.next();
var e; try { r.return(); } catch (x) { e = x; } assert.sameValue(String(e), "closed");
