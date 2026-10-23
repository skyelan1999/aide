package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Stored with the run before any billable POST. Submitting without a remote ID
// is deliberately ambiguous: a resume must not silently create a duplicate.
type MediaGeneration struct {
	Output      string     `json:"output,omitempty"`
	RequestHash string     `json:"requestHash"`
	Voice       string     `json:"voice,omitempty"`
	Kind        string     `json:"kind"`
	Model       string     `json:"model"`
	Identity    string     `json:"identity"`
	State       string     `json:"state"`
	JobID       string     `json:"jobId,omitempty"`
	ResultURL   string     `json:"resultURL,omitempty"`
	Asset       string     `json:"asset,omitempty"`
	Format      string     `json:"format"`
	Duration    int        `json:"duration,omitempty"`
	Usage       TokenUsage `json:"usage,omitempty"`
	Characters  int        `json:"characters,omitempty"`
}

var mediaJobID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var mediaSeconds = regexp.MustCompile(`(?i)([0-9]{1,3})\s*(秒|s\b|seconds?\b)`)
var mediaQuotedText = regexp.MustCompile(`[“「『"]([^”」』"]+)[”」』"]`)

func mediaSpeechText(prompt string) string {
	if _, text, ok := strings.Cut(prompt, "内容："); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	if _, text, ok := strings.Cut(prompt, "内容:"); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	if quoted := mediaQuotedText.FindStringSubmatch(prompt); len(quoted) > 1 {
		return strings.TrimSpace(quoted[1])
	}
	for _, sep := range []string{"内容：", "内容:", "：", ":"} {
		if _, text, ok := strings.Cut(prompt, sep); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return "" // Avoid reading instruction scaffolding aloud as the actual script.
}
func mediaVideoDuration(prompt string) (int, error) {
	seconds := 3
	if match := mediaSeconds.FindStringSubmatch(prompt); len(match) > 1 {
		seconds, _ = strconv.Atoi(match[1])
	}
	for word, value := range map[string]int{"两秒": 2, "二秒": 2, "三秒": 3, "四秒": 4, "五秒": 5, "六秒": 6, "七秒": 7, "八秒": 8, "九秒": 9, "十秒": 10} {
		if strings.Contains(prompt, word) {
			seconds = value
			break
		}
	}
	if seconds < 2 || seconds > 15 {
		return 0, errors.New("当前 Wan2.7 文生视频支持 2–15 秒，请明确此范围内的时长")
	}
	return seconds, nil
}
func nativeMediaEndpoint(cfg Settings, path string) (string, error) {
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("无效的媒体生成基础地址")
	}
	u.Path = path
	u.RawPath = ""
	return u.String(), nil
}
func mediaIdentity(cfg Settings) string {
	identity := cfg.Model + "\n" + cfg.BaseURL + "\n" + routerProviderID(cfg)
	for _, p := range cfg.ModelProviders {
		if p.ID == routerProviderID(cfg) {
			identity += "\n" + p.ActiveKeyID
		}
	}
	return hash([]byte(identity))
}
func mediaJSON(ctx context.Context, client *http.Client, cfg Settings, method, path string, body any) ([]byte, error) {
	endpoint, err := nativeMediaEndpoint(cfg, path)
	if err != nil {
		return nil, err
	}
	var payload io.Reader
	if body != nil {
		b, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, marshalErr
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost && generationModelOutput(cfg.Model) == "video" {
		req.Header.Set("X-DashScope-Async", "enable")
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("媒体接口连接失败；结果可能已提交，请核查远端记录，不要重复创建")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, errors.New("媒体接口响应不完整或超过大小限制")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyMediaError(resp.StatusCode, raw)
	}
	return raw, nil
}
func generateNativeMedia(ctx context.Context, cfg Settings, prompt string, state *MediaGeneration, save func() error, note func(string), client *http.Client, interval time.Duration) error {
	if state.Identity != mediaIdentity(cfg) || state.Model != cfg.Model {
		return errors.New("生成任务的模型、提供商或 Key 已改变；请恢复原配置后继续查询，未创建新任务")
	}
	if state.Asset != "" || state.ResultURL != "" {
		return nil
	}
	if state.State == "failed" {
		return errors.New("远端媒体任务已失败，请核查服务商记录；重新生成需明确发送新任务")
	}
	if state.State == "submitting" && state.JobID == "" {
		return errors.New("上次提交结果尚不明确；请到服务商控制台核查，未自动重复提交")
	}
	if state.Kind == "speech" {
		text := mediaSpeechText(prompt)
		if n := utf8.RuneCountInString(text); n == 0 || n > 600 {
			return errors.New("请用引号或“内容：”给出要合成的 1–600 字文案，例如：生成 MP3，内容：你好，测试成功。")
		}
		if state.State != "" {
			return errors.New("此语音任务已提交，未重复合成")
		}
		state.State = "submitting"
		if err := save(); err != nil {
			return err
		}
		voice := "longanhuan_v3.6"
		if strings.HasPrefix(cfg.Model, "qwen-audio-3.1-") {
			voice = "longanhuan_v3.1"
		}
		if state.Voice != "" {
			voice = state.Voice
		}
		raw, err := mediaJSON(ctx, client, cfg, http.MethodPost, "/api/v1/services/audio/tts/SpeechSynthesizer", map[string]any{"model": cfg.Model, "input": map[string]any{"text": text, "voice": voice, "format": state.Format, "sample_rate": 24000}})
		if err != nil {
			return err
		}
		var result struct {
			Usage struct {
				Input      int `json:"input_tokens"`
				Output     int `json:"output_tokens"`
				Total      int `json:"total_tokens"`
				Characters int `json:"characters"`
			} `json:"usage"`
			Output struct {
				Audio struct {
					URL string `json:"url"`
				} `json:"audio"`
			} `json:"output"`
		}
		if json.Unmarshal(raw, &result) != nil || result.Output.Audio.URL == "" {
			return errors.New("语音 API 未返回音频文件，未重复合成")
		}
		state.Characters = result.Usage.Characters
		state.Usage = TokenUsage{Prompt: result.Usage.Input, Completion: result.Usage.Output, Total: result.Usage.Total, Model: cfg.Model}
		applyBillingIdentity(&state.Usage, cfg)
		state.ResultURL = result.Output.Audio.URL
		state.State = "succeeded"
		return save()
	}
	if state.Kind != "video" {
		return errors.New("尚无此输出类型的原生适配")
	}
	if state.JobID == "" {
		duration, err := mediaVideoDuration(prompt)
		if err != nil {
			return err
		}
		state.Duration = duration
		state.State = "submitting"
		if err := save(); err != nil {
			return err
		}
		raw, err := mediaJSON(ctx, client, cfg, http.MethodPost, "/api/v1/services/aigc/video-generation/video-synthesis", map[string]any{"model": cfg.Model, "input": map[string]any{"prompt": prompt}, "parameters": map[string]any{"resolution": "720P", "ratio": "16:9", "duration": duration, "prompt_extend": false, "watermark": true}})
		if err != nil {
			return err
		}
		var result struct {
			Output struct {
				ID string `json:"task_id"`
			} `json:"output"`
		}
		if json.Unmarshal(raw, &result) != nil || !mediaJobID.MatchString(result.Output.ID) {
			return errors.New("视频提交未返回有效任务 ID；请核查控制台，未重复创建")
		}
		state.JobID = result.Output.ID
		state.State = "pending"
		if err := save(); err != nil {
			return err
		}
	}
	if !mediaJobID.MatchString(state.JobID) {
		return errors.New("视频任务 ID 无效")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := mediaJSON(ctx, client, cfg, http.MethodGet, "/api/v1/tasks/"+state.JobID, nil)
		if err != nil {
			return err
		}
		var result struct {
			Output struct {
				Status string `json:"task_status"`
				URL    string `json:"video_url"`
			} `json:"output"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return errors.New("视频任务状态无效，保留任务 ID，可继续查询")
		}
		switch result.Output.Status {
		case "SUCCEEDED":
			if result.Output.URL == "" {
				return errors.New("视频完成但未返回文件地址")
			}
			state.State = "succeeded"
			state.ResultURL = result.Output.URL
			return save()
		case "FAILED", "CANCELED", "UNKNOWN":
			state.State = "failed"
			if err := save(); err != nil {
				return err
			}
			return fmt.Errorf("视频任务状态 %s；没有自动重新创建", result.Output.Status)
		case "PENDING", "RUNNING":
			state.State = strings.ToLower(result.Output.Status)
			if err := save(); err != nil {
				return err
			}
			if note != nil {
				note("视频生成中 · " + result.Output.Status + "；仅查询原任务，不重复提交")
			}
		default:
			return errors.New("视频任务返回未知状态，保留任务 ID，可继续查询")
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func mediaDownloadURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Port() != "" || !qwenImageAssetHost.MatchString(strings.ToLower(u.Hostname())) {
		return "", errors.New("服务商返回了非受信的媒体存储地址")
	}
	u.Scheme = "https"
	return u.String(), nil
}
func mediaFileMatches(data []byte, format string) bool {
	if format == "wav" {
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
	}
	if format == "mp3" {
		return len(data) >= 3 && (string(data[:3]) == "ID3" || data[0] == 0xff && data[1]&0xe0 == 0xe0)
	}
	return format == "mp4" && len(data) >= 12 && string(data[4:8]) == "ftyp"
}
func (a *App) saveMediaState(task *Task, state *MediaGeneration) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if state.State == "submitting" && state.JobID == "" && task.MediaGeneration != nil && task.MediaGeneration.State != "" {
		return errors.New("此任务已有生成提交记录，未重复提交")
	}
	usageRecorded := task.MediaGeneration != nil && task.MediaGeneration.Usage.Total > 0
	copy := *state
	task.MediaGeneration = &copy
	if state.Usage.Total > 0 && !usageRecorded {
		task.Usage = addUsage(task.Usage, state.Usage)
	}
	for _, s := range a.sessions {
		for _, run := range s.Runs {
			if run == task {
				return a.save(s)
			}
		}
	}
	return errors.New("媒体任务会话已不可用，未继续提交")
}
func (a *App) mediaGenerationChat(ctx context.Context, cfg Settings, input []Message, task *Task, stepIndex int) (string, []Message, error) {
	format := "mp3"
	if strings.Contains(strings.ToLower(taskMediaPrompt(task)), "wav") && !strings.Contains(strings.ToLower(taskMediaPrompt(task)), "mp3") {
		format = "wav"
	}
	return a.mediaGenerationChatRequest(ctx, cfg, input, task, stepIndex, taskMediaPrompt(task), "", format, true)
}
func (a *App) mediaGenerationChatRequest(ctx context.Context, cfg Settings, input []Message, task *Task, stepIndex int, prompt, voice, format string, emit bool) (string, []Message, error) {
	if err := a.sandboxToolError("tts_generate"); err != nil {
		return "", input, err
	}
	if task.Mode != "chat" || emit && len(task.Attachments) > 0 {
		return "", input, errors.New("当前媒体生成仅支持对话模式的文字输入，请移除附件")
	}
	state := MediaGeneration{Kind: generationModelOutput(cfg.Model), Model: cfg.Model, Identity: mediaIdentity(cfg), Format: format, Voice: voice, RequestHash: hash([]byte(prompt + "\n" + voice + "\n" + format))}
	if state.Kind == "video" {
		state.Format = "mp4"
	}
	expectedHash := state.RequestHash
	a.mu.Lock()
	if task.MediaGeneration != nil {
		state = *task.MediaGeneration
	}
	a.mu.Unlock()
	if state.RequestHash != expectedHash {
		return "", input, errors.New("此任务已有不同的媒体生成记录；请发送新任务生成另一份文件，未重复提交")
	}
	save := func() error { return a.saveMediaState(task, &state) }
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	err := generateNativeMedia(ctx, cfg, prompt, &state, save, func(text string) { a.publishStream(task.ID, streamEvent{Event: "note", Text: text, Round: 0}) }, client, 15*time.Second)
	if err != nil {
		a.recordMediaHealth(cfg, err)
		return "", input, err
	}
	a.recordMediaHealth(cfg, nil)
	if state.Asset == "" {
		link, err := mediaDownloadURL(state.ResultURL)
		if err != nil {
			return "", input, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
		if err != nil {
			return "", input, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", input, errors.New("生成完成，下载失败；可继续原任务下载，不需要重新生成")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return "", input, fmt.Errorf("生成完成，下载 HTTP %d；未重新生成", resp.StatusCode)
		}
		limit := int64(20 << 20)
		if state.Kind == "video" {
			limit = 100 << 20
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil || int64(len(data)) > limit || !mediaFileMatches(data, state.Format) {
			return "", input, errors.New("生成文件编码无效或超过保存限制；未重新生成")
		}
		a.filesMu.Lock()
		a.mu.Lock()
		if a.workspaceMode() == "ssh" || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision {
			a.mu.Unlock()
			a.filesMu.Unlock()
			return "", input, errors.New("工作区已变化或为远程工作区；请恢复原工作区后继续下载")
		}
		state.Asset = "generated/" + state.Kind + "/qwen-" + newID() + "." + state.Format
		err = putText(a.workspace, state.Asset, data)
		a.mu.Unlock()
		a.filesMu.Unlock()
		if err != nil {
			state.Asset = ""
			return "", input, err
		}
		if err := save(); err != nil {
			return "", input, err
		}
	}
	label := "音频 " + strings.ToUpper(state.Format)
	if state.Kind == "video" {
		label = "视频 MP4"
	}
	out := "[下载" + label + "](" + state.Asset + ") · 已保存到工作区。"
	if !emit {
		return out, input, nil
	}
	if err := a.saveOutputSegment(task, stepIndex, 0, out); err != nil {
		return out, input, err
	}
	a.publishStream(task.ID, streamEvent{Event: "delta", Text: out, Round: 0})
	return out, input, nil
}

var generatedMediaRawPath = regexp.MustCompile(`^generated/(speech|video)/qwen-[a-f0-9]+\.(mp3|wav|mp4)$`)

func generatedMediaRawType(path string) (string, int64) {
	m := generatedMediaRawPath.FindStringSubmatch(path)
	if len(m) != 3 {
		return "", 0
	}
	if m[1] == "video" && m[2] == "mp4" {
		return "video/mp4", 100 << 20
	}
	if m[1] == "speech" {
		if m[2] == "mp3" {
			return "audio/mpeg", 20 << 20
		}
		if m[2] == "wav" {
			return "audio/wav", 20 << 20
		}
	}
	return "", 0
}
