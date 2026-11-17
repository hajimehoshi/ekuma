// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "normalize of normalized, unnormalized and ill-formed strings"
flags: [noStrict]
---*/

assert.sameValue(String(["é日", "é", "\uD800é\uDC00", "ẛ̣"].map(s => ["NFC", "NFD", "NFKC"].map(f =>
[...s.normalize(f)].map(c => c.charCodeAt(0).toString(16)).join(".")).join(",")).join(" ")), "e9.65e5,65.301.65e5,e9.65e5 e9,65.301,e9 d800.e9.dc00,d800.65.301.dc00,d800.e9.dc00 1e9b.323,17f.323.307,1e69");
