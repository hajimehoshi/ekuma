// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "RegExp results retain independent captures and indices"
flags: [noStrict]
---*/

const re = new RegExp("(?<first>a)" + "(a)".repeat(16 - 1) + "(z)?", "dg");
const input = "a".repeat(16);
const first = re.exec(input);
re.lastIndex = 0;
const second = re.exec(input);
second[1] = "changed";
second.indices[1][0] = 99;
first.push("tail");
first.indices.push([7, 8]);
for (let i = 0; i < 10; i++) first["extra" + i] = i;
assert.sameValue([first[0] === input, first[1], first[16 + 1] === undefined,
 first.at(-1), first.index, first.input === input, first.groups.first,
 first.indices[1].join(), first.indices.groups.first === first.indices[1],
 first.indices[16 + 1] === undefined, first.indices.at(-1).join()].join("|"), "true|a|true|tail|0|true|a|0,1|true|true|7,8");
