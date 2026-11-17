// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "let whose initialization a switch skips"
flags: [noStrict]
---*/

var g;
switch (1) { case 0: let x = 1; case 1: g = () => x; }
var r; try { g(); } catch (e) { r = e.name; } assert.sameValue(String(r), "ReferenceError");
