// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "String of a symbol keeps a lone surrogate of its description"
---*/

assert.sameValue(String(Symbol("\uD800")), "Symbol(\uD800)");
