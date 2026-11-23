// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a function called through call, apply, Reflect.apply or Reflect.construct has the caller and the stack trace of a direct call, however the call is written"
flags: [noStrict]
---*/

var call = Function.prototype.call;

// seen calls run with a function and returns what the function saw when run called it: its
// caller, and the name in the line of the stack trace below its own, with "run" for run.
function seen(run) {
	var caller, line;
	function f() {
		caller = f.caller;
		line = new Error().stack.split("\n")[2];
	}
	run(f);
	var name = /^    at (\S+)/.exec(line)[1];
	return [caller === run ? "run" : caller, name === run.name ? "run" : name];
}

function check(run, caller, name) {
	var got = seen(run);
	assert.sameValue(got[0], caller, run.name + ": caller");
	assert.sameValue(got[1], name, run.name + ": stack trace");
}

check(function callDirect(f) { f.call(null); }, "run", "run");
check(function callGeneric(f) { call.call(f, null); }, "run", "run");
check(function applyArguments(f) { f.apply(this, arguments); }, "run", "run");
check(function applyList(f) { f.apply(null, []); }, "run", "run");
check(function reflectApply(f) { Reflect.apply(f, null, []); }, "run", "run");
check(function reflectConstruct(f) { Reflect.construct(f, []); }, "run", "run");

// A native function that calls through call still shows.
check(function mapThroughCall(f) { [0].map(call, f); }, null, "Array.map");
