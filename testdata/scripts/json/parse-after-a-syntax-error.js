// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "parse after a syntax error"
flags: [noStrict]
---*/

try { JSON.parse('[1,{"a":[2,'); } catch (e) {}
assert.sameValue(String(JSON.stringify(JSON.parse('[3,{"b":4}]'))), "[3,{\"b\":4}]");
