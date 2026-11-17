// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "fresh object per evaluation"
flags: [noStrict]
---*/

// A class expression evaluated twice runs ClassDefinitionEvaluation twice, so each
// class gets its own object.
var classes = [];
for (var i = 0; i < 2; i++) {
	classes.push(class { @((v, c) => { c.metadata.i = i; }) m() {} });
}
var a = classes[0][Symbol.metadata], b = classes[1][Symbol.metadata];
assert.sameValue(String([a !== b, a.i, b.i].join()), "true,0,1");
