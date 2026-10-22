package server

import (
	"errors"
	"fmt"
	"strings"
)

// Deliberately bounded manual surface. No orchestration, management or hidden
// assistant tools; plugin tools have their separate plan executor.
func pwBuiltinAllowed(name string) bool {
	switch name {
	case "list_sources", "list_files", "read_file", "document_search", "mcp_call", "write_file", "run_shell":
		return true
	}
	return false
}
func pwBuiltinDescriptors(disabled []string) []any {
	out := []any{}
	for _, schema := range builtinTools {
		entry, ok := schema.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := entry["function"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if pwBuiltinAllowed(name) && !toolDeniedIn(disabled, name) {
			out = append(out, fn)
		}
	}
	return out
}
func pwBuiltinArguments(name string, args map[string]any) error {
	if !pwBuiltinAllowed(name) {
		return errors.New("PW 不支持此内置工具")
	}
	for _, item := range pwBuiltinDescriptors(nil) {
		fn := item.(map[string]any)
		if fn["name"] != name {
			continue
		}
		schema := fn["parameters"].(map[string]any)
		props := schema["properties"].(map[string]any)
		for key, value := range args {
			spec, ok := props[key].(map[string]any)
			if !ok {
				return fmt.Errorf("未知工具参数 %s", key)
			}
			valid := false
			switch spec["type"] {
			case "string":
				_, valid = value.(string)
			case "object":
				_, valid = value.(map[string]any)
			case "integer":
				v, ok := value.(float64)
				valid = ok && v >= 0 && v == float64(int(v)) && v <= 1000000
			}
			if !valid {
				return fmt.Errorf("工具参数 %s 类型不正确", key)
			}
		}
		required, _ := schema["required"].([]string)
		for _, key := range required {
			value, ok := args[key]
			if !ok {
				return fmt.Errorf("缺少工具参数 %s", key)
			}
			if text, ok := value.(string); ok && key != "content" && strings.TrimSpace(text) == "" {
				return fmt.Errorf("工具参数 %s 不能为空", key)
			}
		}
		return nil
	}
	return errors.New("内置工具未注册")
}
