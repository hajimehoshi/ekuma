// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "RegExp literal searches preserve UTF-16 offsets and independent match result properties"
---*/

var input = "😀ab😀ab";
var unicode = /😀/uy;
unicode.lastIndex = 1;
var match = unicode.exec(input);
assert.sameValue(match.index, 0);
assert.sameValue(unicode.lastIndex, 2);

var sticky = /😀/y;
sticky.lastIndex = 1;
assert.sameValue(sticky.exec(input), null);
assert.sameValue(sticky.lastIndex, 0);

var global = /ab/g;
global.lastIndex = 1;
match = global.exec(input);
assert.sameValue(match.index, 2);
assert.sameValue(global.lastIndex, 4);
match.length = 0;
match.push("new");
match.groups = 42;
assert.sameValue(match[0], "new");
assert.sameValue(match.input, input);
assert.sameValue(match.index, 2);
assert.sameValue(match.groups, 42);

assert.sameValue(input.replace(global, "-"), "😀-😀-");
assert.sameValue(RegExp.leftContext, "😀ab😀");
assert.sameValue("\uD83Dab\uDE00".replace(/ab/g, ""), "😀");
assert.sameValue("😀".replace("\uDE00", ""), "\uD83D");
