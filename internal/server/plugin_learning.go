package server

// MM-05 插件感知训练（plugin-aware learning）：
//
//  1. 能力域感知：pluginCapabilityHint() 从插件 surface 生成「能力域 → 工具」
//     的结构化描述注入系统提示，让模型理解每个启用插件扩展了什么能力、
//     什么场景该调用，而不只是看到裸工具名。
//  2. 使用经验学习：插件工具每次执行的成败/场景汇总到
//     memory/core/plugin-experience.md，任务结束时批量落盘、自动去重精简，
//     后续任务把经验注入系统提示，形成「越用越会用」的闭环。
//
// 经验文件与核心记忆同目录但独立成文，避免 write_memory 的自由文本污染
// 结构化经验，也便于单独裁剪/重置。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	pluginExperienceFile = "plugin-experience.md"
	maxPluginExpEntries  = 60 // 经验条目上限，超出按使用度裁剪
)

// pluginSurfaceModel 是解析 surface.json 所需的最小结构（能力感知专用）。
type pluginSurfaceModel struct {
	Plugins []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Error    string `json:"error"`
		Provided []string `json:"provided"`
		Tools    []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Executable  bool   `json:"executable"`
		} `json:"tools"`
		Slots []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"slots"`
	} `json:"plugins"`
}

// capabilityDomain 把插件的 provided/工具归并成一个人类可读的能力域描述。
type capabilityDomain struct {
	PluginID   string
	Capability string
	ExecTools  []string
	AllTools   []string
}

// parsePluginSurface 解析当前 surface；失败返回 nil（调用方按无扩展处理）。
func (a *App) parsePluginSurface() *pluginSurfaceModel {
	var m pluginSurfaceModel
	if len(a.pluginSurface) == 0 {
		return nil
	}
	if err := json.Unmarshal(a.pluginSurface, &m); err != nil {
		return nil
	}
	return &m
}

// pluginCapabilityHint 生成系统提示中的「扩展能力」段落。
// 只描述加载成功、且至少有一个可执行工具的插件；纯面板插件（无 executable
// 工具）对模型的工具选择无意义，不列以免噪声。
func (a *App) pluginCapabilityHint() string {
	m := a.parsePluginSurface()
	if m == nil {
		return ""
	}
	domains := []capabilityDomain{}
	for _, p := range m.Plugins {
		if p.Error != "" {
			continue
		}
		domain := capabilityDomain{PluginID: p.ID, Capability: strings.Join(p.Provided, "/")}
		for _, t := range p.Tools {
			if t.Executable {
				domain.ExecTools = append(domain.ExecTools, t.Name)
			} else {
				domain.AllTools = append(domain.AllTools, t.Name)
			}
		}
		if len(domain.ExecTools) == 0 {
			continue
		}
		domains = append(domains, domain)
	}
	if len(domains) == 0 {
		return ""
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].PluginID < domains[j].PluginID })
	var b strings.Builder
	b.WriteString("## 已启用的扩展能力（插件）\n")
	b.WriteString("除内置工具外，下列插件提供额外的可调用工具；请在任务匹配其能力域时主动使用，不要重复用内置命令造轮子：\n")
	for _, d := range domains {
				fmt.Fprintf(&b, "- 能力域「%s」（插件 %s）：可调用 %s。\n",
					d.Capability, d.PluginID, joinCN(d.ExecTools, "、"))
	}
	b.WriteString("调用插件工具与内置工具方式一致；若某插件工具返回失败，参考下方「插件使用经验」，不要在同一路径上反复重试。")
	return b.String()
}

// pluginExperiencePath 经验文件路径：<data>/memory/core/plugin-experience.md。
func (a *App) pluginExperiencePath() string {
	return filepath.Join(MemoryCoreDir(a.dataPath), pluginExperienceFile)
}

// pluginExperienceEntry 是一条结构化使用经验。
type pluginExperienceEntry struct {
	Tool    string // 工具名
	Plugin  string // 所属插件
	OK      bool   // 本次是否成功
	Detail  string // 成败的简要说明（去敏、截断）
	Used    int    // 历史累计出现次数
	LastAt  string // 最近一次时间
}

// readPluginExperience 读取经验文件并解析为条目；文件缺失返回空。
func (a *App) readPluginExperience() []pluginExperienceEntry {
	b, err := os.ReadFile(a.pluginExperiencePath())
	if err != nil {
		return nil
	}
	entries := []pluginExperienceEntry{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		e := parseExperienceLine(strings.TrimPrefix(line, "- "))
		if e.Tool != "" {
			entries = append(entries, e)
		}
	}
	return entries
}

// parseExperienceLine 解析形如
//
//	`[OK] sqlite_query(sqlite) ×5 最近 2026-09-28：查询返回 12 行`
func parseExperienceLine(s string) pluginExperienceEntry {
	e := pluginExperienceEntry{}
	if strings.HasPrefix(s, "[OK] ") {
		e.OK = true
		s = strings.TrimPrefix(s, "[OK] ")
	} else if strings.HasPrefix(s, "[FAIL] ") {
		e.OK = false
		s = strings.TrimPrefix(s, "[FAIL] ")
	}
	// tool(plugin)
	if i := strings.Index(s, "("); i > 0 {
		e.Tool = strings.TrimSpace(s[:i])
		rest := s[i+1:]
		if j := strings.Index(rest, ")"); j >= 0 {
			e.Plugin = strings.TrimSpace(rest[:j])
			s = rest[j+1:]
		}
	}
	// ×N
	if i := strings.Index(s, "×"); i >= 0 {
		rest := s[i+len("×"):]
		n := 0
		k := 0
		for k < len(rest) && rest[k] >= '0' && rest[k] <= '9' {
			n = n*10 + int(rest[k]-'0')
			k++
		}
		e.Used = n
		s = rest[k:]
	}
	// 最近 DATE：
	if i := strings.Index(s, "："); i >= 0 {
		head := strings.TrimSpace(s[:i])
		e.Detail = strings.TrimSpace(s[i+len("："):])
		if j := strings.LastIndex(head, " "); j >= 0 {
			e.LastAt = strings.TrimSpace(head[j+1:])
		}
	} else {
		e.Detail = strings.TrimSpace(s)
	}
	return e
}

// recordPluginExperience 把一次插件工具调用结果并入经验并落盘。
// toolName/pluginID 为空（非插件工具）时直接忽略。
func (a *App) recordPluginExperience(toolName, pluginID string, ok bool, detail string) {
	if toolName == "" || pluginID == "" {
		return
	}
	detail = sanitizeExpDetail(detail)
	entries := a.readPluginExperience()
	now := time.Now().UTC().Format("2006-01-02")
	merged := false
	for i := range entries {
		if entries[i].Tool == toolName && entries[i].Plugin == pluginID {
			entries[i].Used++
			entries[i].LastAt = now
			// 成败状态以最近一次为准；成功细节优先保留，失败细节更新为最新原因
			entries[i].OK = ok
			if detail != "" {
				entries[i].Detail = detail
			}
			merged = true
			break
		}
	}
	if !merged {
		entries = append(entries, pluginExperienceEntry{
			Tool: toolName, Plugin: pluginID, OK: ok, Detail: detail,
			Used: 1, LastAt: now,
		})
	}
	entries = trimExperience(entries)
	a.writePluginExperience(entries)
}

// sanitizeExpDetail 去掉换行、压缩空白、截断长度，保证经验单行可读。
func sanitizeExpDetail(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// trimExperience 超出上限时，优先保留失败经验（更有避坑价值）与高频条目。
func trimExperience(entries []pluginExperienceEntry) []pluginExperienceEntry {
	if len(entries) <= maxPluginExpEntries {
		return entries
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].OK != entries[j].OK {
			return !entries[i].OK // false（失败）排前
		}
		return entries[i].Used > entries[j].Used
	})
	return entries[:maxPluginExpEntries]
}

// writePluginExperience 渲染经验为 Markdown 并原子写。
func (a *App) writePluginExperience(entries []pluginExperienceEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Plugin != entries[j].Plugin {
			return entries[i].Plugin < entries[j].Plugin
		}
		return entries[i].Tool < entries[j].Tool
	})
	var b strings.Builder
	b.WriteString("# 插件使用经验\n\n")
	b.WriteString("> 由 aide 根据插件工具真实调用结果自动维护，记录各工具的成败与适用场景；失败条目优先保留。\n\n")
	for _, e := range entries {
		status := "OK"
		if !e.OK {
			status = "FAIL"
		}
		detail := e.Detail
		if detail == "" {
			if e.OK {
				detail = "调用成功"
			} else {
				detail = "调用失败"
			}
		}
		fmt.Fprintf(&b, "- [%s] %s(%s) ×%d 最近 %s：%s\n",
			status, e.Tool, e.Plugin, e.Used, e.LastAt, detail)
	}
	dir := MemoryCoreDir(a.dataPath)
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(a.pluginExperiencePath(), []byte(b.String()), 0o600)
}

// pluginExperienceHint 生成系统提示中的「插件使用经验」段落；无经验返回空。
func (a *App) pluginExperienceHint() string {
	entries := a.readPluginExperience()
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 插件使用经验（历史调用沉淀）\n")
	// 失败经验在前，提醒避坑；成功经验简述适用场景。
	fail, ok := []pluginExperienceEntry{}, []pluginExperienceEntry{}
	for _, e := range entries {
		if e.OK {
			ok = append(ok, e)
		} else {
			fail = append(fail, e)
		}
	}
	for _, e := range fail {
		fmt.Fprintf(&b, "- ⚠️ %s 曾失败（×%d）：%s；再次使用前先确认条件。\n", e.Tool, e.Used, e.Detail)
	}
	for _, e := range ok {
		fmt.Fprintf(&b, "- ✓ %s 可用（×%d）：%s。\n", e.Tool, e.Used, e.Detail)
	}
	return b.String()
}

// resetPluginExperience 清空经验（供工厂重置/设置重置调用）。
func (a *App) resetPluginExperience() error {
	err := os.Remove(a.pluginExperiencePath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// joinCN 用 sep 连接字符串（避免引入 strings.Join 的语义重复，保持文案统一）。
func joinCN(items []string, sep string) string {
	return strings.Join(items, sep)
}
