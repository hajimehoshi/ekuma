// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "const assignment"
flags: [noStrict]
---*/

var r; const k = 1; try { k = 2; } catch (e) { r = e.constructor.name; } assert.sameValue(String(r + k), "TypeError1");
