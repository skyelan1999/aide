package server

import (
	"context"
	"encoding/json"
	"log"
	"sync/atomic"
)

// Only the known read-only weather tools join the curated assistant tool set.
// Other installed plugins and future weather operations remain executor-only.
func assistantWeatherTool(name string) bool {
	switch name {
	case "weather_search_locations", "weather_location", "weather_forecast":
		return true
	}
	return false
}
func (a *App) assistantWeatherTools() []any {
	tools := []any{}
	for _, tool := range a.pluginToolSchemasForOwner("weather") {
		schema, _ := tool.(map[string]any)
		fn, _ := schema["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if assistantWeatherTool(name) {
			tools = append(tools, tool)
		}
	}
	return tools
}
func (a *App) assistantWeatherQuery(ctx context.Context, name string, args map[string]any) string {
	result, err := a.callPluginToolContext(ctx, "weather", name, args)
	if err != nil {
		return "天气查询失败：" + err.Error()
	}
	if _, proposals := normalizePluginResult(result); len(proposals) != 0 {
		return "天气查询失败：只读工具返回了变更提案，未执行或登记任何变更。"
	}
	if proof, _ := ctx.Value(nativeWeatherEvidenceKey{}).(*nativeWeatherEvidence); proof != nil && name == "weather_forecast" {
		proof.forecast.Store(true)
		log.Printf("[assistant-call] stage=weather-plugin tool=weather_forecast result=ok")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "天气查询失败：返回格式无效。"
	}
	return string(data)
}

// Per-helper evidence: only executing the forecast plugin can set this proof.
// It is not accepted from model text, tool arguments or older call history.
type nativeWeatherEvidenceKey struct{}
type nativeWeatherEvidence struct{ forecast atomic.Bool }
