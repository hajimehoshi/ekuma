// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "template literal and tagged template"
flags: [noStrict]
---*/

function tag(s, ...v) { return s.raw.join('-') + v.join(''); } assert.sameValue(String(`a${1 + 1}b` + tag`x${1}y${2}z`), "a2bx-y-z12");
