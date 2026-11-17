// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

var assert = {
    sameValue(actual, expected) {
        if (actual === expected) {
            // Equal zeros have the same sign when their reciprocals match.
            if (actual !== 0 || 1 / actual === 1 / expected) return;
        } else if (actual !== actual && expected !== expected) {
            return;
        }
        throw new Test262Error("Values differ: actual=" + String(actual) + ", expected=" + String(expected));
    }
};
