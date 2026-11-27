// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A global store of a local variable that other code can change reads the variable after resolving the reference"
flags: [noStrict, noRepeat]
noRepeatReason: "the stores create the global properties, which a second evaluation would find without a lookup"
---*/

var log = [];
var onHas = {};
Object.setPrototypeOf(globalThis, new Proxy({}, {
	has(target, key) {
		if (typeof key === "string" && Object.prototype.hasOwnProperty.call(onHas, key)) {
			log.push("has " + key);
			return onHas[key]();
		}
		return Reflect.has(target, key);
	},
}));

(function() {
	var value = 1;
	onHas.closure = function() { value = 2; return false; };
	closure = value;
})();
assert.sameValue(globalThis.closure, 2);

(function(value) {
	onHas.mapped = () => { arguments[0] = 2; return false; };
	mapped = value;
})(1);
assert.sameValue(globalThis.mapped, 2);

assert.sameValue(log.join(), "has closure,has mapped");
