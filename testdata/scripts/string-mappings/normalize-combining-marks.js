// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Combining marks stay attached across normalization chunks"
flags: [noStrict]
---*/

assert.sameValue("e\u0301".repeat(50000).normalize("NFC") === "\u00e9".repeat(50000), true);
