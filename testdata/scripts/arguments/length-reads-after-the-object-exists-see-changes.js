// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Repeated reads of the length of arguments after the arguments object exists see each change to its length property"
---*/

var step = 0;
Object.defineProperty(Object.prototype, "9", {
	get() {
		step++;
		if (step === 2) {
			this.length = 7;
		} else if (step === 3) {
			Object.defineProperty(this, "length", {
				get() {
					return "accessor";
				},
				configurable: true,
			});
		} else if (step === 4) {
			delete this.length;
		}
		return 0;
	},
	configurable: true,
});

function lengths() {
	var r = [];
	for (var i = 0; i < 5; i++) {
		r.push(arguments.length);
		arguments[9];
	}
	return r.join();
}
assert.sameValue(lengths(1, 2), "2,2,7,accessor,");

delete Object.prototype[9];
