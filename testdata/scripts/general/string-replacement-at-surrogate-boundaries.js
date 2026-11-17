// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string replacement at surrogate boundaries"
flags: [noStrict]
---*/

assert.sameValue(String(JSON.stringify(["😀".replace("\ud83d", "_"), "😀😀".replaceAll("\ude00", "_"),
	"😀".replaceAll("", "_"), "😀".replace("\ude00", "[$&][$\u0060][$']")])), "[\"_\\ude00\",\"\\ud83d_\\ud83d_\",\"_\\ud83d_\\ude00_\",\"\\ud83d[\\ude00][\\ud83d][]\"]");
