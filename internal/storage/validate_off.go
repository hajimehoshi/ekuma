// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !ekumavalidate

package storage

// ValidateExec says whether the interpreter checks the code that it runs, as validate_on.go
// describes.
const ValidateExec = false

func CheckValue(v Value) {}

func (r *RealmPart) checkArrayIteration() {}

func (r *RealmPart) CheckNoIteratorReturn() {}

func (r *RealmPart) CheckPlainArrayIteration(*Object) {}

func (r *RealmPart) checkUnmodifiedRegExp(*Object) {}

func (r *RealmPart) checkRegExpShape(*Object) {}

func (s *Shape) checkEnumerable() {}

func (c *sparseIndexCache) checkSparseIndices(*Shape) {}

func (r *RealmPart) checkArrayProtoPlain() {}
