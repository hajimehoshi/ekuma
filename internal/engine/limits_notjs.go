// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package engine

// maxDepth is the number of nested interpreter loops at which a RangeError is thrown.
const maxDepth = 400

// maxBufferLength is the largest length of an ArrayBuffer in bytes. It keeps every valid
// integer index of a typed array an array index.
const maxBufferLength = 1<<32 - 1
