// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Reads of arguments see the changes to the arguments object that a forwarding call exposed"
---*/

const receiver = {
	apply(_, args) {
		args[0] = 7;
		args.length = 5;
	},
};

function invoke(x) {
	receiver.apply(null, arguments);
	return [arguments[0], arguments.length].join();
}
assert.sameValue(invoke(1), "7,5");
