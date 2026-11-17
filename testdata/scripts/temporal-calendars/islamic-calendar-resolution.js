// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "islamic calendar resolution"
flags: [noStrict]
---*/

assert.sameValue(String(new Intl.DateTimeFormat("en-u-ca-islamic").resolvedOptions().calendar), "islamic-tbla");
