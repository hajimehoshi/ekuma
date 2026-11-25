// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// ProxyState holds the internal slots of a proxy object.
// A detached global proxy has a nil target. A revoked proxy has both slots nil.
type ProxyState struct {
	Target  *Object
	Handler *Object
}

func (s *ProxyState) IsGlobal() bool {
	return s.Handler != nil && s.Handler.Class == ClassGlobalProxyHandler
}

func (s *ProxyState) AllowsGlobalAccess(group *Object) bool {
	return s.Target != nil && s.Handler == group
}
