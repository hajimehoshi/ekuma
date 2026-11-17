// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Uppercase expansion across chunks"
flags: [noStrict]
---*/

assert.sameValue("\u00df".repeat(50000).toUpperCase() === "SS".repeat(50000), true);
