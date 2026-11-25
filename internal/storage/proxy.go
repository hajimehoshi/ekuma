// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// ProxyState holds the internal slots of a proxy object.
// A detached global proxy has a nil target. A revoked proxy has both slots nil.
type ProxyState struct {
	Target  *Object
	Handler *Object
}

// NewProxyObject is [NewExoticObject] for a proxy, whose [[Call]] and [[Construct]] are fn. fn is
// nil for a proxy that is not callable.
func NewProxyObject(s *Shape, state *ProxyState, fn *Function) *Object {
	return (&PropertyObject{
		Kind:  KindObject,
		Class: ClassProxy,
		flags: ObjExotic,
		shape: s,
		Fn:    fn,
		Ext:   state,
	}).Init()
}

func (s *ProxyState) IsGlobal() bool {
	return s.Handler != nil && s.Handler.Class == ClassGlobalProxyHandler
}

func (s *ProxyState) AllowsGlobalAccess(group *Object) bool {
	return s.Target != nil && s.Handler == group
}
