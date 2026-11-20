// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "ASCII uppercase letters are valid in the collation option"
---*/

const collator = new Intl.Collator("de", {collation: "PHONEBK"});
assert.sameValue(collator.resolvedOptions().collation, "phonebk");
