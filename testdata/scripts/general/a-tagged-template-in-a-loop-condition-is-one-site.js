// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a tagged template in the condition of a loop gets the same template object in every iteration"
---*/

var templates = new Set();
function tag(strings) {
  templates.add(strings);
  return true;
}
for (var i = 0; i < 3 && tag`x`; i++) {}
var k = 3;
while (tag`y` && +k) k--;
assert.sameValue(templates.size, 2);
