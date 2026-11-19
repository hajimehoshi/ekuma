// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "destructuring declaration source survives its bindings"
flags: [noStrict]
---*/

assert.sameValue((function () {
	var i = 0;
	var { constructor: i, toFixed: j } = i;
	return j;
})(), Number.prototype.toFixed);
