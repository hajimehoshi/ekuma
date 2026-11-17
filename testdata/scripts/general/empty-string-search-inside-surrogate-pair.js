// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "empty string search inside surrogate pair"
flags: [noStrict]
---*/

assert.sameValue(String(["😀".indexOf("", 1), "😀".lastIndexOf("", 1)].join()), "1,1");
