// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Deleting a global identifier respects the binding resolved by a prototype proxy"
flags: [noStrict]
---*/

function noop() {}
function remove() {
	let calls = 0;
	Object.setPrototypeOf(globalThis, new Proxy({}, {
		has(target, key) {
			if (key === "missingAtResolution") {
				noop(...Array(8192));
				++calls;
				globalThis[key] = 1;
				return false;
			}
			if (key === "lexicalAtDeletion") {
				++calls;
				$262.evalScript("let lexicalAtDeletion = 2;");
				return true;
			}
			return Reflect.has(target, key);
		}
	}));

	assert.sameValue((delete missingAtResolution) && calls, 1);
	assert.sameValue(globalThis.missingAtResolution, 1);
	assert.sameValue(delete lexicalAtDeletion, false);
	assert.sameValue(calls, 2);
}

remove();
