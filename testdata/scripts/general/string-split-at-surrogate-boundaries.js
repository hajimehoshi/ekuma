// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string split at surrogate boundaries"
flags: [noStrict]
---*/

assert.sameValue(String(JSON.stringify(["😀".split("\ud83d"), "😀".split("\ude00"),
"😀x😀".split("\ude00x\ud83d"), "😀😀".split("\ud83d", 2)])), "[[\"\",\"\\ude00\"],[\"\\ud83d\",\"\"],[\"\\ud83d\",\"\\ude00\"],[\"\",\"\\ude00\"]]");
