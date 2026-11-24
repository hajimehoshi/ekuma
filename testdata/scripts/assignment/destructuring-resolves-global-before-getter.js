// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A destructuring target resolves before the source getter creates the global"
flags: [onlyStrict]
---*/

const source = {
	get value() {
		globalThis.createdByGetter = 1;
		return 2;
	}
};
// A prior configurable property stays configurable across a var declaration.
globalThis.createdByGetter = 0;
let caught;
try {
	$262.evalScript(`
		"use strict";
		var createdByGetter;
		delete globalThis.createdByGetter;
		({ value: createdByGetter } = source);
	`);
} catch (e) {
	caught = e;
}
assert.sameValue(caught instanceof ReferenceError, true);
assert.sameValue(globalThis.createdByGetter, 1);
