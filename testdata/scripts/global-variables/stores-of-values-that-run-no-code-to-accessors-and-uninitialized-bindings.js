// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Global stores of values that run no code call a global setter every time and reject an uninitialized lexical binding"
flags: [noStrict, noRepeat]
noRepeatReason: "the global lexical declaration is accepted only once in a realm"
---*/

var calls = [];
Object.defineProperty(globalThis, "accessor", {
	get() { return 0; },
	set(v) { calls.push(v, this === globalThis); },
	configurable: true,
});
function store(v) { accessor = v; }
function storeStrict(v) { "use strict"; accessor = v; }
store(1);
store(2);
storeStrict(3);
storeStrict(4);
assert.sameValue(calls.join(), "1,true,2,true,3,true,4,true");

function storeLater(v) { laterLexical = v; }
var beforeInitialization;
$262.evalScript(`
	try {
		storeLater(1);
	} catch (e) {
		beforeInitialization = e.constructor.name;
	}
	let laterLexical = 0;
`);
assert.sameValue(beforeInitialization, "ReferenceError");
storeLater(2);
storeLater(3);
assert.sameValue(laterLexical, 3);
assert.sameValue(Object.getOwnPropertyDescriptor(globalThis, "laterLexical"), undefined);
