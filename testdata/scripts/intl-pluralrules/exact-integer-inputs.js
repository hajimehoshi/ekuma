// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "plural selection preserves the low digits of decimal strings and BigInts"
---*/

const plural = new Intl.PluralRules("ru");
const value = "100000000000000000021";
assert.sameValue(plural.select(value), "one");
assert.sameValue(plural.select(BigInt(value)), "one");
assert.sameValue(plural.select(Number(value)), "many");
