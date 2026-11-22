// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Unicode 17 case mappings and final sigma context"
flags: [noStrict]
---*/

// Beria Erfe letters are cased and the combining double caron is case-ignorable.
assert.sameValue("\u{16ebb}".toUpperCase(), "\u{16ea0}");
assert.sameValue("\u{16ea0}\u1acfΣ".toLowerCase(), "\u{16ebb}\u1acfς");
