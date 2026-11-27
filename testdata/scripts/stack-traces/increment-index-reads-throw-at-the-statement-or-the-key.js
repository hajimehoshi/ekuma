// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a[++i] and a[i++] throw at the start of the statement when the counter cannot be converted and at the key when the read fails, before and after reads that succeed"
flags: [noStrict]
---*/

// where returns the line and column of the function that f throws in.
function where(f) {
	try {
		f();
	} catch (e) {
		return /:(\d+:\d+)\)?$/.exec(e.stack.split("\n")[1])[1];
	}
}

function prefix(a, i) {
	var v = 0;
	v +=
		a[
			++i];
	return v;
}
function postfix(a, i) {
	var v = 0;
	v +=
		a[
			i++];
	return v;
}
for (var n = 0; n < 3; n++) {
	assert.sameValue(where(() => prefix([1, 2], Symbol())), "20:2");
	assert.sameValue(where(() => prefix(null, 0)), "22:4");
	assert.sameValue(prefix([1, 2], 0), 2);
	assert.sameValue(where(() => postfix([1, 2], Symbol())), "27:2");
	assert.sameValue(where(() => postfix(null, 0)), "29:4");
	assert.sameValue(postfix([1, 2], 0), 1);
}
