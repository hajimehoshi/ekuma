// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "cached matchers distinguish both the pattern and the flags"
---*/

const input = "A/B ia/b";
assert.sameValue(new RegExp("ia/b").exec(input)[0], "ia/b");
assert.sameValue(new RegExp("a/b", "i").exec(input)[0], "A/B");
assert.sameValue(new RegExp("a/b").exec(input)[0], "a/b");
