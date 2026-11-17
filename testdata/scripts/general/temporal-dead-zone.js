// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "temporal dead zone"
flags: [noStrict]
---*/

var r; try { x; let x = 1; } catch (e) { r = e instanceof ReferenceError; } assert.sameValue(String(r), "true");
