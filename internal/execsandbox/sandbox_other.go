//go:build !linux

package execsandbox

import "fmt"

func ABI() (int, error) { return 0, Available() }
func Available() error {
	return fmt.Errorf("当前平台没有接入内核沙箱，受限命令未执行；可使用Linux Docker工作台")
}
func runChild(policy, string) error { return Available() }
