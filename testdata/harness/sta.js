// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

class Test262Error extends Error {
    constructor(message) {
        super(message);
        this.name = "Test262Error";
    }
}
