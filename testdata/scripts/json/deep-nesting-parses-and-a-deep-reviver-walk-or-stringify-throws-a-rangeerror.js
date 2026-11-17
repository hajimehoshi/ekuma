// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "deep nesting parses and a deep reviver walk or stringify throws a RangeError"
flags: [noStrict]
---*/

var n = 100000, a = JSON.parse("[".repeat(n) + "]".repeat(n)), d = 0;
for (var x = a; x.length; x = x[0]) d++;
var r = [d];
try { JSON.stringify(a); } catch (e) { r.push(e instanceof RangeError); }
try { JSON.parse("[".repeat(n) + "]".repeat(n), function(k, v) { return v; }); } catch (e) { r.push(e instanceof RangeError); }
assert.sameValue(String(r.join()), "99999,true,true");
