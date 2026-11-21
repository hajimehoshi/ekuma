// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Search ignores sort collations and preserves other supported locale extensions"
---*/

const options = new Intl.Collator("en-US-u-co-emoji-kn", {
  usage: "search",
  collation: "eor",
}).resolvedOptions();

assert.sameValue(options.locale, "en-US-u-kn");
assert.sameValue(options.collation, "default");
