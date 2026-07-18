package storage

import "testing"

// 多策略行为由 NewTestStoragePolicyManagerMulti 覆盖，见 manager_test.go。
func TestMultiPoliciesListOrder(t *testing.T) {
	mgr := NewTestStoragePolicyManagerMulti("b", map[string]StorageDriver{
		"a": &mockDriver{},
		"b": &mockDriver{},
		"c": &mockDriver{},
	})
	list := mgr.ListPolicies()
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	if list[0].Name != "b" || !list[0].IsDefault {
		t.Errorf("default first: %+v", list[0])
	}
}
