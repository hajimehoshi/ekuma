// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "ASCII uppercase mapping across chunks"
flags: [noStrict]
---*/

assert.sameValue("x".repeat(1 << 17).toUpperCase() === "X".repeat(1 << 17), true);
