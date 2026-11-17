// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the next method of another kind of built-in iterator throws"
flags: [noStrict]
---*/

var it = new Set([1]).values();
it.next = new Map().keys().next;
var r;
try { for (var x of it) {} r = "no error"; } catch (e) { r = e instanceof TypeError; }
assert.sameValue(String(r), "true");
