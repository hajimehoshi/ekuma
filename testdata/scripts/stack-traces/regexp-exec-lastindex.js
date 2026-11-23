// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Implicit RegExp exec calls preserve the native frame for observable lastIndex operations"
---*/

const rx = /x/g;
let frame;
const index = {
	valueOf() {
		frame = new Error().stack.split("\n")[2];
		return 0;
	},
};
rx.lastIndex = index;
rx.exec("x");
const execFrame = frame;
frame = undefined;
rx.lastIndex = index;
rx.test("x");
assert.sameValue(frame, execFrame);

const separator = /x/;
separator.constructor = { [Symbol.species]: function () { return rx; } };
frame = undefined;
rx.lastIndex = index;
"".split(separator);
assert.sameValue(frame, execFrame);

const frozen = Object.freeze(/x/g);
function failedWriteFrame(method) {
	try {
		frozen[method]("x");
	} catch (error) {
		assert.sameValue(error.constructor, TypeError);
		return error.stack.split("\n")[1];
	}
	throw new Test262Error("expected a write to the frozen lastIndex");
}
assert.sameValue(failedWriteFrame("test"), failedWriteFrame("exec"));
