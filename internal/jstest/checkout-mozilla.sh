#!/usr/bin/env bash
# SPDX-License-Identifier: 0BSD
# SPDX-FileCopyrightText: 2026 Hajime Hoshi

set -eu

v8=${1:?expected the path to the V8 checkout}
data="$v8/test/mozilla/data"
revision=$(awk -F"'" '/\/mozilla-tests[.]git/ { print $(NF-1) }' "$v8/DEPS")
if [[ ! $revision =~ ^[0-9a-f]{40}$ ]]; then
  echo "Expected the mozilla-tests commit SHA in $v8/DEPS" >&2
  exit 1
fi
git init -q "$data"
git -C "$data" fetch --depth 1 https://chromium.googlesource.com/v8/deps/third_party/mozilla-tests.git "$revision"
git -C "$data" -c core.autocrlf=false checkout --detach FETCH_HEAD
