package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// A business flow is a reusable set of existing goals, not another executor.
// Each stage uses normal tool execution, file approval and operator acceptance.
type PlanFlowStage struct {
	ID     string            `json:"id"`
	Recipe string            `json:"recipe"`
	Stage  string            `json:"stage"`
	Tools  map[string]string `json:"tools,omitempty"`
}
type FlowFile struct {
	Source string `json:"source,omitempty"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}
type FlowReceipt struct {
	Tool      string `json:"tool"`
	Digest    string `json:"digest"`
	Result    string `json:"result"`
	Truncated bool   `json:"truncated"`
}
type FlowInput struct {
	PlanID    string        `json:"planId"`
	RunID     string        `json:"runId"`
	Title     string        `json:"title"`
	Snapshot  string        `json:"snapshot"`
	Output    string        `json:"output"`
	Truncated bool          `json:"truncated"`
	Files     []FlowFile    `json:"files"`
	Receipts  []FlowReceipt `json:"receipts"`
}
type flowRecipeStep struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Deliverable string   `json:"deliverable"`
	Checks      []string `json:"checks"`
}
type flowRecipe struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Steps []flowRecipeStep `json:"steps"`
}

func businessRecipes() []flowRecipe {
	return []flowRecipe{
		{"software", "软件维护", []flowRecipeStep{
			{"understand", "理解问题", "问题范围与预期行为", []string{"已记录实际问题、预期行为和影响范围", "缺少输入已明确列出"}},
			{"reproduce", "复现与影响分析", "复现记录与影响清单", []string{"复现步骤和实际结果可核对", "相关代码、文档和测试已复核"}},
			{"change", "实现修改", "可审查的代码与文件差异", []string{"修改对应已确认的问题范围", "文件已应用且未覆盖无关工作"}},
			{"verify", "回归验证", "实际测试结果与剩余限制", []string{"已运行相关回归并核对结果", "失败和未执行项已单独记录"}},
			{"deliver", "审查与交付", "交付说明与审查材料", []string{"交付物、差异和测试证据可追溯", "外部PR或发布状态按实际回执记录"}},
		}},
		{"engineering", "工程交付", []flowRecipeStep{
			{"inputs", "核对工程资料", "资料来源、版本与缺口清单", []string{"输入来源和适用版本已经核对", "缺少参数和约束已明确记录"}},
			{"analyze", "计算与分析", "计算依据、假设与结果", []string{"计算输入、单位与依据可追溯", "未确认假设和适用边界已列出"}},
			{"produce", "生成报告或图纸", "可编辑的报告或图纸文件", []string{"内容对应已确认输入和计算结果", "交付文件已实际保存"}},
			{"check", "校核成果", "一致性与完整性校核记录", []string{"资料、计算和成果内容一致性已检查", "缺陷和待核实项已明确记录"}},
			{"revise", "修订与交付", "修订成果与交付清单", []string{"校核缺口已处理或明确阻塞", "实际文件、版本和交付状态可核对"}},
		}},
	}
}
func (a *App) flowToolCatalogLocked() map[string]string {
	var surface struct {
		Plugins []struct {
			ID    string           `json:"id"`
			Error string           `json:"error"`
			Tools []map[string]any `json:"tools"`
		} `json:"plugins"`
	}
	_ = json.Unmarshal(a.pluginSurface, &surface)
	enabled := map[string]string{}
	for _, p := range a.pluginRegistry.Plugins {
		if p.Enabled {
			enabled[p.ID] = p.Version
		}
	}
	out := map[string]string{}
	for _, p := range surface.Plugins {
		v, ok := enabled[p.ID]
		if !ok || p.Error != "" {
			continue
		}
		for _, t := range p.Tools {
			name := stringValue(t["name"])
			exec, _ := t["executable"].(bool)
			if name != "" && exec && !toolDeniedIn(a.settings.DisabledTools, name) {
				out[name] = recoveryHash([]any{p.ID, v, t})
			}
		}
	}
	return out
}
func flowView(s *Session, ws string, catalog map[string]string, choices []map[string]string, language string) map[string]any {
	names := []string{}
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	stages := map[string]any{}
	for _, item := range s.Plan {
		if item.Flow != nil && item.WorkspaceID == ws {
			selected := []string{}
			for name := range item.Flow.Tools {
				selected = append(selected, name)
			}
			sort.Strings(selected)
			stages[item.ID] = map[string]any{"tools": selected, "status": item.Status}
		}
	}
	return map[string]any{"workspaceId": ws, "snapshot": recoveryHash([]any{ws, s.Plan}), "recipes": businessRecipesLanguage(language), "language": language, "tools": names, "toolChoices": choices, "stages": stages}
}
func (a *App) businessFlowAPI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Language    string              `json:"language"`
		Action      string              `json:"action"`
		ItemID      string              `json:"itemId"`
		Tools       []string            `json:"tools"`
		WorkspaceID string              `json:"workspaceId"`
		Snapshot    string              `json:"snapshot"`
		Recipe      string              `json:"recipe"`
		Brief       string              `json:"brief"`
		StageTools  map[string][]string `json:"stageTools"`
	}
	if r.Method != "GET" {
		if err := decode(w, r, &in); err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.workspaceConfigMu.RLock()
	defer a.workspaceConfigMu.RUnlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loadLockStateLocked().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	s := a.sessions[r.PathValue("id")]
	if s == nil || s.Deleted {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	ws := a.wsID()
	catalog := a.flowToolCatalogLocked()
	if r.Method == "GET" {
		jsonOut(w, 200, flowView(s, ws, catalog, a.flowToolChoicesLocked(catalog), r.URL.Query().Get("language")))
		return
	}
	if in.WorkspaceID != ws || in.Snapshot != recoveryHash([]any{ws, s.Plan}) {
		fail(w, 409, errors.New("规划已变化，请刷新后核对"))
		return
	}
	if in.Action == "configure" {
		item := a.findPlanItemLocked(s, in.ItemID)
		if item == nil || item.Flow == nil || item.WorkspaceID != ws || item.Status == "closed" || len(in.Tools) > 8 {
			fail(w, 400, errors.New("无效流程阶段或工具过多"))
			return
		}
		run := planRun(s, item)
		if run != nil && (run.Status == "running" || run.Status == "awaiting_approval" || run.Status == "awaiting_clarification" || run.Status == "paused") {
			fail(w, 409, errors.New("请先结束当前阶段执行再修改工具"))
			return
		}
		tools := map[string]string{}
		for _, name := range in.Tools {
			if catalog[name] == "" {
				fail(w, 409, errors.New("所选插件工具已不可用，请刷新"))
				return
			}
			tools[name] = catalog[name]
		}
		before := *item
		oldUpdated := s.Updated
		undo := flowInvalidationRollback(s)
		copy := *item.Flow
		copy.Tools = tools
		item.Flow = &copy
		if recoveryHash(before.Flow.Tools) != recoveryHash(tools) && run != nil {
			run.GoalInputsStale = true
			item.Status = "blocked"
		}
		item.Updated = time.Now().UTC().Format(time.RFC3339Nano)
		s.Updated = item.Updated
		a.reconcileFlowInputsLocked(s)
		if err := a.save(s); err != nil {
			*item = before
			undo()
			s.Updated = oldUpdated
			fail(w, 500, err)
			return
		}
		a.broadcastSessionsChanged(s.ID)
		jsonOut(w, 200, map[string]any{"item": item})
		return
	}
	if in.Action != "" && in.Action != "create" {
		fail(w, 400, errors.New("未知流程操作"))
		return
	}
	if strings.TrimSpace(in.Brief) == "" || len([]rune(in.Brief)) > 1400 {
		fail(w, 400, errors.New("业务目标须为1–1400字"))
		return
	}
	var recipe flowRecipe
	for _, v := range businessRecipesLanguage(in.Language) {
		if v.ID == in.Recipe {
			recipe = v
		}
	}
	if recipe.ID == "" || len(s.Plan)+len(recipe.Steps) > planItemLimit {
		fail(w, 400, errors.New("未知流程或超出规划上限"))
		return
	}
	stages := map[string]bool{}
	for _, step := range recipe.Steps {
		stages[step.ID] = true
	}
	for id, tools := range in.StageTools {
		if !stages[id] || len(tools) > 8 {
			fail(w, 400, errors.New("无效流程阶段或工具过多"))
			return
		}
		for _, name := range tools {
			if catalog[name] == "" {
				fail(w, 409, errors.New("所选插件工具已不可用，请刷新"))
				return
			}
		}
	}
	added := []PlanItem{}
	group := "flow-" + newID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, step := range recipe.Steps {
		contract := &GoalContract{Deliverables: []string{step.Deliverable}}
		for _, text := range step.Checks {
			contract.Criteria = append(contract.Criteria, GoalCriterion{Text: text})
		}
		var contractErr error
		contract, contractErr = freshGoalContract(contract)
		if contractErr != nil {
			fail(w, 500, contractErr)
			return
		}
		flow := &PlanFlowStage{ID: group, Recipe: recipe.ID, Stage: step.ID, Tools: map[string]string{}}
		for _, name := range in.StageTools[step.ID] {
			flow.Tools[name] = catalog[name]
		}
		item := PlanItem{ID: "plan-" + newID(), Title: recipe.Name + " · " + step.Title, Detail: strings.TrimSpace(in.Brief), WorkspaceID: ws, Source: "user", Status: "pending", Contract: contract, Flow: flow, Created: now, Updated: now}
		if len(added) > 0 {
			item.DependsOn = []string{added[len(added)-1].ID}
		}
		added = append(added, item)
	}
	oldUpdated := s.Updated
	s.Plan = append(s.Plan, added...)
	s.Updated = now
	if err := a.save(s); err != nil {
		s.Plan = s.Plan[:len(s.Plan)-len(added)]
		s.Updated = oldUpdated
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 201, map[string]any{"items": added, "flowId": group})
}
func flowInputSnapshot(item *PlanItem, run *Task) string {
	return recoveryHash([]any{item.ID, item.Contract, goalRunFingerprint(run)})
}
func (a *App) collectFlowInputsLocked(s *Session, item *PlanItem, resuming ...bool) ([]FlowInput, error) {
	if item == nil || item.Flow == nil {
		return nil, nil
	}
	if prior := planRun(s, item); prior != nil {
		if recoveryRetryUnknown(prior) && !(len(resuming) > 0 && resuming[0]) {
			return nil, errors.New("阶段存在未核查效果，请先核查并使用原执行恢复入口，不得重发阶段")
		}
		if prior.Status == "awaiting_approval" {
			return nil, errors.New("请先处理当前阶段待审批文件")
		}
	}
	if err := validateFlowStage(item.Flow); err != nil {
		return nil, err
	}
	catalog := a.flowToolCatalogLocked()
	for name, signature := range item.Flow.Tools {
		if signature == "" || catalog[name] != signature {
			return nil, fmt.Errorf("流程工具 %s 已停用或版本/定义变化，请重新配置流程", name)
		}
	}
	if len(item.DependsOn) > 8 {
		return nil, errors.New("流程输入最多8项")
	}
	inputs := []FlowInput{}
	for _, id := range item.DependsOn {
		dep := a.findPlanItemLocked(s, id)
		run := planRun(s, dep)
		if dep == nil || dep.WorkspaceID != a.wsID() || !goalDependencyReady(s, dep) || run == nil {
			return nil, errors.New("流程输入尚未验收或项目不匹配")
		}
		input := FlowInput{PlanID: id, RunID: run.ID, Title: dep.Title, Snapshot: flowInputSnapshot(dep, run), Files: []FlowFile{}, Receipts: []FlowReceipt{}}
		for _, step := range run.Steps {
			if step.Status == "completed" && step.Content != "" {
				input.Output = step.Content
			}
		}
		input.Truncated = len([]rune(input.Output)) > 4000
		input.Output = truncateRunes(input.Output, 4000)
		for _, f := range run.Files {
			if len(input.Files) >= 40 {
				input.Truncated = true
				break
			}
			if f.Applied {
				input.Files = append(input.Files, FlowFile{Source: f.Source, Path: f.Path, Digest: hash([]byte(f.Content))})
			}
		}
		for _, use := range run.ToolUses {
			if len(input.Receipts) >= 10 {
				input.Truncated = true
				break
			}
			input.Receipts = append(input.Receipts, FlowReceipt{Tool: use.Tool, Digest: outcomeDigest(use), Result: truncateRunes(use.Result, 1000), Truncated: len([]rune(use.Result)) > 1000})
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}
func validateFlowStage(f *PlanFlowStage) error {
	if f == nil {
		return nil
	}
	if !pluginPlanID(f.ID) || !pluginPlanID(f.Recipe) || !pluginPlanID(f.Stage) || len(f.Tools) > 8 {
		return errors.New("无效流程阶段")
	}
	for name, sig := range f.Tools {
		if name == "" || len(name) > 200 || len(sig) > 128 {
			return errors.New("无效流程工具")
		}
	}
	return nil
}
func invalidateFlowInputsLocked(s *Session) {
	// Monotonic invalidation: changing a predecessor never silently re-accepts
	// an old downstream result, even if the predecessor is later marked passed.
	for pass := 0; pass < len(s.Plan); pass++ {
		changed := false
		for i := range s.Plan {
			item := &s.Plan[i]
			run := planRun(s, item)
			if run == nil || run.GoalInputsStale || len(run.FlowInputs) == 0 {
				continue
			}
			for _, input := range run.FlowInputs {
				var dep *PlanItem
				for j := range s.Plan {
					if s.Plan[j].ID == input.PlanID {
						dep = &s.Plan[j]
						break
					}
				}
				if dep == nil || !goalDependencyReady(s, dep) || flowInputSnapshot(dep, planRun(s, dep)) != input.Snapshot {
					run.GoalInputsStale = true
					if item.Status != "closed" {
						item.Status = "blocked"
					}
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
}
func (a *App) reconcileFlowInputsLocked(s *Session) { invalidateFlowInputsLocked(s) }

func flowInputsCurrent(s *Session, run *Task) bool {
	if run.GoalInputsStale {
		return false
	}
	for _, input := range run.FlowInputs {
		var dep *PlanItem
		for i := range s.Plan {
			if s.Plan[i].ID == input.PlanID {
				dep = &s.Plan[i]
				break
			}
		}
		if dep == nil || !goalDependencyReady(s, dep) || flowInputSnapshot(dep, planRun(s, dep)) != input.Snapshot {
			return false
		}
	}
	return true
}
func flowInvalidationRollback(s *Session) func() {
	flags := map[*Task]bool{}
	statuses := map[string]string{}
	for _, r := range s.Runs {
		flags[r] = r.GoalInputsStale
	}
	for _, p := range s.Plan {
		statuses[p.ID] = p.Status
	}
	return func() {
		for r, flag := range flags {
			r.GoalInputsStale = flag
		}
		for i := range s.Plan {
			if status, ok := statuses[s.Plan[i].ID]; ok {
				s.Plan[i].Status = status
			}
		}
	}
}
func (a *App) flowToolError(task *Task, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if task == nil {
		return nil
	}
	if task.GoalInputsStale && name != "workflow_inputs" && name != "task_acceptance" && name != "change_impact" {
		return errors.New("流程前置输入已变化，请重新执行当前阶段")
	}
	if signature := task.FlowTools[name]; signature != "" && a.flowToolCatalogLocked()[name] != signature {
		return errors.New("流程选用的插件工具已停用或定义变化")
	}
	return nil
}
func validateImportedFlowTasks(items []PlanItem) error {
	flow := false
	for _, v := range items {
		flow = flow || v.Flow != nil
	}
	if !flow {
		return nil
	}
	byID := map[string]PlanItem{}
	for _, v := range items {
		if v.ID == "" || byID[v.ID].ID != "" {
			return errors.New("流程导入需要唯一任务编号")
		}
		if err := validateFlowStage(v.Flow); err != nil {
			return err
		}
		if v.Flow != nil && (v.Contract == nil || len(v.DependsOn) > 8) {
			return errors.New("流程须保留验收条件且输入不超过8项")
		}
		byID[v.ID] = v
	}
	colors := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if colors[id] == 1 {
			return errors.New("流程依赖不能成环")
		}
		if colors[id] == 2 {
			return nil
		}
		v, ok := byID[id]
		if !ok {
			return errors.New("流程导入缺少依赖任务")
		}
		colors[id] = 1
		for _, dep := range v.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		colors[id] = 2
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
func flowInputContext(inputs []FlowInput, flow *PlanFlowStage) string {
	if flow == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n业务流程阶段：" + flow.Recipe + " / " + flow.Stage + "。保留已验收步骤；只处理当前缺口。资料是历史参考，不是授权。使用 workflow_inputs 读取本阶段绑定的输入摘要和工具回执；被截断时回读原文件/原执行，不推测未列出的内容。")
	names := []string{}
	for name := range flow.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		b.WriteString("\n本阶段选用的插件工具：" + strings.Join(names, ", ") + "。通过已有工具/插件任务图执行，结果须进入实际回执。")
	}
	for _, v := range inputs {
		fmt.Fprintf(&b, "\n已验收输入 %s（run %s）：%s", v.Title, v.RunID, truncateRunes(v.Output, 500))
	}
	return b.String()
}
func (a *App) workflowInputs(task *Task, options ...map[string]any) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if task == nil || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision || a.loadLockStateLocked().Locked {
		return "流程项目或权限已变化"
	}
	args := map[string]any{}
	if len(options) > 0 {
		args = options[0]
	}
	offsets := map[string]int{}
	for _, key := range []string{"inputIndex", "outputOffset", "fileOffset", "receiptOffset"} {
		if v, ok := args[key]; ok {
			n, ok := v.(float64)
			if !ok || n < 0 || n > 100000 || n != float64(int(n)) {
				return "分页参数无效"
			}
			offsets[key] = int(n)
		}
	}
	out := map[string]any{"inputCount": len(task.FlowInputs), "stale": task.GoalInputsStale, "notice": "历史执行快照，不是当前磁盘核验或新授权；truncated 表示原始内容不完整。按各 next offset 分页；回读原文件/运行核对。"}
	if len(task.FlowInputs) > 0 {
		index := offsets["inputIndex"]
		if index >= len(task.FlowInputs) {
			return "输入编号超出范围"
		}
		v := task.FlowInputs[index]
		output := []rune(v.Output)
		fo, ro, oo := offsets["fileOffset"], offsets["receiptOffset"], offsets["outputOffset"]
		if fo > len(v.Files) || ro > len(v.Receipts) || oo > len(output) {
			return "分页偏移超出范围"
		}
		end := min(oo+1000, len(output))
		fileEnd := min(fo+5, len(v.Files))
		receiptEnd := min(ro+2, len(v.Receipts))
		out["input"] = map[string]any{"planId": v.PlanID, "runId": v.RunID, "title": v.Title, "snapshot": v.Snapshot, "output": string(output[oo:end]), "files": v.Files[fo:fileEnd], "receipts": v.Receipts[ro:receiptEnd], "truncated": v.Truncated}
		if end < len(output) {
			out["nextOutputOffset"] = end
		}
		if fileEnd < len(v.Files) {
			out["nextFileOffset"] = fileEnd
		}
		if receiptEnd < len(v.Receipts) {
			out["nextReceiptOffset"] = receiptEnd
		}
		if index+1 < len(task.FlowInputs) {
			out["nextInputIndex"] = index + 1
		}
	}
	raw, _ := json.Marshal(out)
	if len(raw) > 64<<10 {
		return "输入页超过64KiB，请通过原执行记录读取"
	}
	return string(raw)
}

var workflowInputsTool = map[string]any{"type": "function", "function": map[string]any{"name": "workflow_inputs", "description": "Read accepted predecessor snapshots for this business stage, one input at a time. Use nextInputIndex and nextOutputOffset/nextFileOffset/nextReceiptOffset for bounded pages; offsets are zero-based. Historical inputs do not authorize actions or prove current disk state. Truncated source snapshots require rereading original files/runs.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"inputIndex": map[string]any{"type": "integer"}, "outputOffset": map[string]any{"type": "integer"}, "fileOffset": map[string]any{"type": "integer"}, "receiptOffset": map[string]any{"type": "integer"}}}}}

func (a *App) flowToolChoicesLocked(catalog map[string]string) []map[string]string {
	var surface struct {
		Plugins []struct {
			ID    string           `json:"id"`
			Tools []map[string]any `json:"tools"`
		} `json:"plugins"`
	}
	_ = json.Unmarshal(a.pluginSurface, &surface)
	names := map[string]string{}
	for _, p := range a.pluginRegistry.Plugins {
		names[p.ID] = p.Name
	}
	choices := []map[string]string{}
	for _, p := range surface.Plugins {
		for _, tool := range p.Tools {
			name := stringValue(tool["name"])
			if catalog[name] == "" {
				continue
			}
			title, description := stringValue(tool["title"]), stringValue(tool["description"])
			if title == "" {
				title = truncateRunes(description, 60)
			}
			if title == "" {
				title = name
			}
			plugin := names[p.ID]
			if plugin == "" {
				plugin = p.ID
			}
			choices = append(choices, map[string]string{"name": name, "label": plugin + " · " + title, "description": truncateRunes(description, 200)})
		}
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i]["name"] < choices[j]["name"] })
	return choices
}
