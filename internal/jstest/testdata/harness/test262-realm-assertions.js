// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

function realmAssertions(realms) {
    function prototype(value, realm, type, label) {
        const expected = realms[realm].global[type].prototype;
        let observed = typeof value;
        if (value === null) {
            observed = "null";
        } else if (typeof value === "object" || typeof value === "function") {
            const actual = Object.getPrototypeOf(value);
            if (actual === expected) return;

            observed = "unrecognized prototype";
            if (actual !== null) {
                const name = Object.getOwnPropertyDescriptor(actual, "constructor")?.value?.name;
                for (const [id, { global }] of Object.entries(realms)) {
                    if (actual === global[type].prototype) {
                        observed = `${id}.${type}.prototype`;
                        break;
                    }
                    if (name && actual === global[name]?.prototype) {
                        observed = `${id}.${name}.prototype`;
                        break;
                    }
                }
            }
        }
        throw new Test262Error(`${label}: expected ${realm}.${type}.prototype, observed ${observed}`);
    }

    function throws(fn, realm, type, label) {
        try {
            fn();
        } catch (error) {
            prototype(error, realm, type, label);
            return;
        }
        throw new Test262Error(`${label}: expected ${realm}.${type}.prototype, no exception thrown`);
    }

    return { prototype, throws };
}
