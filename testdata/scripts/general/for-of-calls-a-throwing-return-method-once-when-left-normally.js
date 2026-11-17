// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "for-of calls a throwing return method once when left normally"
flags: [noStrict]
---*/

var n = 0;
var it = { [Symbol.iterator]() { return { next() { return { done: false }; }, return() { n++; throw 1; } }; } };
try { for (var x of it) break; } catch (e) {}
outer: for (var i = 0; i < 2; i++) { try { for (var x of it) continue outer; } catch (e) {} }
assert.sameValue(String(n), "3");
