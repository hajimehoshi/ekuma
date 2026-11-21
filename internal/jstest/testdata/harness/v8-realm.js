// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

(function (create, lookup) {
  "use strict";
  const {Error, SyntaxError, TypeError} = globalThis;
  function isObject(value) {
    return value !== null && (typeof value === "object" || typeof value === "function");
  }

  function select(i) {
    if (typeof i !== "number") throw new Error("Invalid argument");
    const realm = lookup(i | 0);
    if (!realm) throw new Error("Invalid realm index");
    return realm;
  }

  globalThis.Realm = {
    create(options) {
      let ownQueue;
      try {
        ownQueue = isObject(options) && options.create_own_microtask_queue;
      } catch {
        // d8 continues with the default queue when reading the option throws.
      }
      if (ownQueue) {
        throw new Error("Separate realm microtask queues are unsupported");
      }
      return create(false);
    },
    createAllowCrossRealmAccess() {
      return create(true);
    },
    eval(i, source, options) {
      const realm = select(i);
      let type = "string";
      if (isObject(options)) {
        type = options.type;
        if (typeof type === "string") {
          // d8 reads the arguments option even for string sources.
          void options.arguments;
        }
      }
      if (type !== "string" || typeof source !== "string") {
        throw new Error("Invalid argument");
      }
      return realm.evaluate(source, SyntaxError);
    },
    global(i) {
      return select(i).global;
    },
  };

  // Foreign globals expose no own keys. Reads of well-known symbols return
  // undefined, including the toStringTag read by Object.prototype.toString.
  const wellKnown = new Set(Object.getOwnPropertyNames(Symbol).map(k => Symbol[k])
      .filter(v => typeof v === "symbol"));
  const isWellKnown = wellKnown.has.bind(wellKnown);
  function deny() { throw new TypeError("no access"); }
  return new Proxy(Object.create(null), {
    __proto__: null,
    get(_, key) {
      if (isWellKnown(key)) return undefined;
      return deny();
    },
    set: deny,
    has: deny,
    deleteProperty: deny,
    defineProperty: deny,
    getOwnPropertyDescriptor: deny,
    setPrototypeOf: deny,
    preventExtensions: deny,
  });
})
