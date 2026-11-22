// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Unicode 18 case mappings and final sigma context"
flags: [noStrict]
---*/

// Barred A is cased and U+1ADE is case-ignorable.
assert.sameValue("\u{1df41}".toUpperCase(), "\u{1df40}");
assert.sameValue("\u{1df40}\u1adeΣ".toLowerCase(), "\u{1df41}\u1adeς");
