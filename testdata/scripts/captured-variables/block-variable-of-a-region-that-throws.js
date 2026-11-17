// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "block variable of a region that throws"
flags: [noStrict]
---*/

var f;
try { let v = 1; f = () => v; v = 2; throw "T"; } catch (e) {}
assert.sameValue(String(f()), "2");
