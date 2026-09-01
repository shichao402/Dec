package servicehost

import "testing"

func TestLockedInvokeWhitelistIsNarrow(t *testing.T) {
	allowed := []string{
		"probe_remote_host",
		"ensure_remote_service",
		"configure_remote_service",
		"list_managed_devices",
		"register_managed_device",
		"remove_managed_device",
	}
	for _, method := range allowed {
		if !invokeAllowedWhenLocked(method) {
			t.Fatalf("连接前设备生命周期方法 %q 应允许", method)
		}
	}

	blocked := []string{
		"load_device_summary",
		"load_global_settings",
		"list_secrets",
		"connect_repo",
		"save_enabled_bundles",
	}
	for _, method := range blocked {
		if invokeAllowedWhenLocked(method) {
			t.Fatalf("资产/配置方法 %q 不得在锁定态放行", method)
		}
	}
}

func TestLockedOperationWhitelistOnlyAllowsProvision(t *testing.T) {
	if !operationAllowedWhenLocked("provision_remote_host") {
		t.Fatal("连接前必须允许远端置备")
	}
	for _, operation := range []string{"pull", "push", "delete", "scan_managed_projects"} {
		if operationAllowedWhenLocked(operation) {
			t.Fatalf("操作 %q 不得在锁定态放行", operation)
		}
	}
}
