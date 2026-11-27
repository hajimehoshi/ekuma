// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Local prepends preserve cached lengths, surrogate pairs, and earlier values"
---*/

function build() {
	const suffix = "xyz".repeat(16);
	let value = suffix;
	for (let i = 0; i < 24; i++) {
		value = (i < 20 ? "é" : "\uDE00") + value;
		assert.sameValue(value.length, suffix.length + i + 1);
	}
	value = "\uD83D" + value;
	const previous = value;
	for (let i = 0; i < 24; i++) {
		value = "ab" + value;
	}
	assert.sameValue(value, "ab".repeat(24) + previous);
	assert.sameValue(previous, "\u{1F600}" + "\uDE00".repeat(3) + "é".repeat(20) + suffix);
	assert.sameValue(previous.codePointAt(0), 0x1F600);
}

function readsAfterEvaluatingPrefix() {
	let value = "original";
	value = `${value = "updated"}` + value;
	return value;
}

build();
assert.sameValue(readsAfterEvaluatingPrefix(), "updatedupdated");
