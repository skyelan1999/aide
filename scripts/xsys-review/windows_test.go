//go:build windows

package xsysreview

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIssue303WorkingSetFlags(t *testing.T) {
	for bit, read := range map[uint]func(windows.PSAPI_WORKING_SET_EX_BLOCK) bool{
		15: windows.PSAPI_WORKING_SET_EX_BLOCK.Shared,
		22: windows.PSAPI_WORKING_SET_EX_BLOCK.Locked,
		23: windows.PSAPI_WORKING_SET_EX_BLOCK.LargePage,
		31: windows.PSAPI_WORKING_SET_EX_BLOCK.Bad,
	} {
		if read(0) || read(1) || !read(1<<bit) {
			t.Fatalf("working-set bit %d", bit)
		}
	}
}

func TestIssue302DescriptorFailures(t *testing.T) {
	// Memory is valid; revision zero makes the descriptor invalid. The API's
	// failure must not become the sentinel for a successfully read absent ACL.
	invalid := new(windows.SECURITY_DESCRIPTOR)
	valid, err := windows.NewSecurityDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]func() (*windows.ACL, bool, error){
		{invalid.DACL, valid.DACL}, {invalid.SACL, valid.SACL},
	} {
		if _, _, err := pair[0](); err == nil || errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
			t.Fatalf("invalid descriptor: %v", err)
		}
		if _, _, err := pair[1](); !errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
			t.Fatalf("absent ACL: %v", err)
		}
	}
}

func TestIssue305MissingPrivilege(t *testing.T) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_ADJUST_PRIVILEGES, &token); err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	// An unassigned, synthetic LUID cannot enable a real privilege or modify
	// the account; Windows reports BOOL success plus ERROR_NOT_ALL_ASSIGNED.
	requested := windows.Tokenprivileges{PrivilegeCount: 1}
	requested.Privileges[0] = windows.LUIDAndAttributes{Luid: windows.LUID{LowPart: 0x7ffffffe}, Attributes: windows.SE_PRIVILEGE_ENABLED}
	if err := windows.AdjustTokenPrivileges(token, false, &requested, 0, nil, nil); !errors.Is(err, windows.ERROR_NOT_ALL_ASSIGNED) {
		t.Fatalf("missing privilege: %v", err)
	}
}
