// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

(function (create, lookup, current) {
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

  function change(i, operation) {
    if (!select(i).change(operation)) throw new Error("Invalid realm index");
  }

  globalThis.Realm = {
    current,
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
    detachGlobal(i) {
      change(i, "detachGlobal");
    },
    dispose(i) {
      change(i, "dispose");
    },
    switch(i) {
      change(i, "switch");
    },
    navigate(i) {
      change(i, "navigate");
      return i | 0;
    },
    navigateSameOrigin(i) {
      change(i, "navigateSameOrigin");
      return i | 0;
    },
    eval(i, source, options) {
      let realm = select(i);
      let type = "string";
      if (isObject(options)) {
        type = options.type;
        if (typeof type === "string") {
          // d8 reads the arguments option even for string sources.
          void options.arguments;
        }
        // Reading the options can dispose or replace the selected realm.
        realm = select(i);
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
