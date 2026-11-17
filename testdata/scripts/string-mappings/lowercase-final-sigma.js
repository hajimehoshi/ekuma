// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Final sigma casing uses context across chunks"
flags: [noStrict]
---*/

assert.sameValue("\u0391\u03a3".repeat(50000).toLowerCase() === "\u03b1\u03c3".repeat(49999) + "\u03b1\u03c2", true);
