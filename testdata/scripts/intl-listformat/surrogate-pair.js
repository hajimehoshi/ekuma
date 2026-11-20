// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "surrogates in list elements form a pair when the pattern inserts no separator"
---*/

const format = new Intl.ListFormat("ja", {type: "unit", style: "narrow"});
assert.sameValue(format.format(["\uD83D", "", "\uDE00"]), "😀");
