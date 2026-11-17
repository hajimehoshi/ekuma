// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a gap whose copies form a pair"
flags: [noStrict]
---*/

assert.sameValue(String(JSON.stringify([[1]], null, "\uDC00\uD800").split("\n")[2] === "\uDC00𐀀\uD8001"), "true");
