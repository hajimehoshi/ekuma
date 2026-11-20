// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A parenthesized optional member keeps its receiver as a tag"
---*/

const object = { tag() { return this; } };
assert.sameValue((object?.tag)``, object);
