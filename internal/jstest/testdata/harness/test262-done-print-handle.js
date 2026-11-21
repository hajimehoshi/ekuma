// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

function $DONE(error) {
    if (error !== undefined) {
        print("Test262:AsyncTestFailure:" + String(error));
        return;
    }
    print("Test262:AsyncTestComplete");
}
