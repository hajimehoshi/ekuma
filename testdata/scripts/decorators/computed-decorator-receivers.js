// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Class and member decorators selected from arguments receive the arguments object"
---*/

function record(_, context) {
	this[1].push(context.kind);
}

function decorateClass() {
	const index = 0;
	@(arguments[index]) class C {}
}

function decorateMember() {
	class C {
		@(arguments[0]) method() {}
	}
}
const calls = [];
decorateClass(record, calls);
decorateMember(record, calls);
assert.sameValue(calls.join(), "class,method");
