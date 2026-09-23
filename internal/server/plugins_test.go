package server

import (
	"encoding/json"
	"strings"
	"testing"
)

const validPlugin = `'use strict';
module.exports = {
  name: 'hello-aide',
  apply(ctx) {
    ctx.logger.info('loaded');
    ctx.tool({ name: 'hello', description: '向用户问好' });
    ctx.slot({ id: 'hello-panel', name: '问好面板' });
  },
};
`

const factoryPlugin = `'use strict';
module.exports = () => ({
  name: 'factory-plugin',
  apply(ctx) { ctx.provide('factory.service'); },
});
`

func TestPluginUploadAndValidation(t *testing.T) {
	a := testApp(t)
	// 合法插件上传
	w := request(a, "POST", "/api/plugins", map[string]any{"id": "hello", "name": "示例插件", "description": "演示", "code": validPlugin})
	requireStatus(t, w, 201)
	// id 重复 → 409
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "hello", "name": "x", "code": validPlugin}), 409)
	// 语法错误 → 400
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"name": "bad", "code": "module.exports = {"}), 400)
	// 形态错误（无 apply）→ 400
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"name": "noshape", "code": "module.exports = { name: 'x' };"}), 400)
	// 工厂函数形态 → 201
	w = request(a, "POST", "/api/plugins", map[string]any{"name": "工厂", "code": factoryPlugin})
	requireStatus(t, w, 201)
	// 超过 1 个参数的工厂 → 400
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"name": "inject", "code": "module.exports = (a, b) => ({ apply() {} });"}), 400)
	// 代码超限 → 400
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"name": "big", "code": strings.Repeat("x", maxPluginCode+1)}), 400)
}

func TestPluginLifecycleAndSurface(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "hello", "name": "示例插件", "code": validPlugin}), 201)
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "factory", "name": "工厂插件", "code": factoryPlugin}), 201)

	// 列表含两个插件且默认启用
	w := request(a, "GET", "/api/plugins", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"id":"hello"`) || !strings.Contains(w.Body.String(), `"id":"factory"`) {
		t.Fatalf("plugin list: %s", w.Body.String())
	}
	// surface 聚合工具/槽位/服务
	w = request(a, "GET", "/api/plugin-surface", nil)
	requireStatus(t, w, 200)
	body := w.Body.String()
	if !strings.Contains(body, `"id":"hello"`) || !strings.Contains(body, "问好面板") || !strings.Contains(body, "factory.service") {
		t.Fatalf("surface: %s", body)
	}
	// 停用 hello → surface 不再含 hello
	requireStatus(t, request(a, "PUT", "/api/plugins/hello", map[string]any{"enabled": false}), 200)
	w = request(a, "GET", "/api/plugin-surface", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), `"id":"hello"`) {
		t.Fatalf("disabled plugin still in surface: %s", w.Body.String())
	}
	// 重新启用
	requireStatus(t, request(a, "PUT", "/api/plugins/hello", map[string]any{"enabled": true}), 200)
	w = request(a, "GET", "/api/plugin-surface", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"id":"hello"`) {
		t.Fatalf("re-enabled plugin missing from surface: %s", w.Body.String())
	}
	// 未知插件操作 → 404
	requireStatus(t, request(a, "PUT", "/api/plugins/ghost", map[string]any{"enabled": true}), 404)
	requireStatus(t, request(a, "DELETE", "/api/plugins/ghost", nil), 404)
	// 删除 → 列表消失
	requireStatus(t, request(a, "DELETE", "/api/plugins/factory", nil), 200)
	w = request(a, "GET", "/api/plugins", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), `"id":"factory"`) {
		t.Fatalf("deleted plugin still listed: %s", w.Body.String())
	}
}

func TestPluginApplyErrorIsolated(t *testing.T) {
	a := testApp(t)
	bad := `'use strict';
module.exports = { name: 'broken', apply() { throw new Error('boom'); } };
`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "broken", "name": "坏插件", "code": bad}), 201)
	w := request(a, "GET", "/api/plugins", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("apply error not reported: %s", w.Body.String())
	}
	// 坏插件不阻断其他插件
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "ok", "name": "好插件", "code": validPlugin}), 201)
	w = request(a, "GET", "/api/plugin-surface", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"id":"ok"`) {
		t.Fatalf("good plugin missing: %s", w.Body.String())
	}
	var surface struct {
		Plugins []struct {
			ID    string `json:"id"`
			Error string `json:"error"`
		} `json:"plugins"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &surface)
	if len(surface.Plugins) != 2 {
		t.Fatalf("want 2 surface entries, got %d", len(surface.Plugins))
	}
}
