// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Word segments use ICU rule status, including across dictionary breaks"
---*/

const words = new Intl.Segmenter("ja", { granularity: "word" });
assert.sameValue(words.segment("½").containing(0).isWordLike, false);
assert.sameValue(words.segment("々").containing(0).isWordLike, false);

// The dictionary splits after 漢. The final combining mark changes the
// status of both segments.
function statuses(input) {
    return Array.from(words.segment(input), part => part.isWordLike).join(",");
}
assert.sameValue(statuses("漢々"), "true,true");
assert.sameValue(statuses("漢々\u0301"), "false,false");
