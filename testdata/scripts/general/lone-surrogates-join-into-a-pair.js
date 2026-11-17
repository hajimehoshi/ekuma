// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "lone surrogates join into a pair"
flags: [noStrict]
---*/

assert.sameValue(String(("\uD83D" + "\uDE00") === "\u{1F600}"), "true");
