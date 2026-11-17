// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "labelled continue"
flags: [noStrict]
---*/

var r = ""; a: for (var i = 0; i < 3; i++) { for (var j = 0; j < 3; j++) { if (j == 1) continue a; r += i + "" + j; } } assert.sameValue(String(r), "001020");
