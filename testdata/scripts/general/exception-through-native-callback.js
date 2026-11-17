// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "exception through native callback"
flags: [noStrict]
---*/

var r; try { [1, 2].forEach(function(x) { if (x == 2) throw new Error("boom"); }); } catch (e) { r = e.message; } assert.sameValue(String(r), "boom");
