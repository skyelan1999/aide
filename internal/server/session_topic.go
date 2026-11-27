package server

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const topicTitleLimit = 32

func defaultTopicTitle(title string) bool {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "", "新会话", "新聊天", "new conversation", "new session", "new chat":
		return true
	}
	return false
}

var topicFiller = regexp.MustCompile(`(?i)^(?:请|麻烦|那就)?(?:你好呀?|您好|嗨|谢谢|好的?|可以|收到|明白|继续(?:吧|一下|执行|上次(?:的)?(?:任务|工作)|之前的(?:任务|工作))?|接着(?:做|来)|做完了?吗|做了吗|好了?吗|怎么样了|进度如何|再试一次|重试|可以了|好了吗|ok(?:ay)?|hi|hello|thanks|continue|go on|carry on|try again|any update[s]?)[\s。？！.!?，,]*$`)
var genericTopicTitle = regexp.MustCompile(`(?i)^(?:你好[，,。！!]|您好[，,。！!]|(?:好的?|明白了?)[，,。！!]|我(?:理解(?:您|你)|将(?:为|帮)(您|你)|会(?:为|帮)(您|你))|请问(?:您|你)|(?:您|你)指的是什么|工具调用中[：:]|(?:sure|hello|of course)[!,. ]|(?:i understand|how can i help)\b)`)
var statusTopicPhrase = regexp.MustCompile(`(?i)^(?:任务(?:执行|处理|进度(?:更新|查询)?|继续|总结)|继续(?:执行|完成|处理|推进)(?:之前的?|当前|上次的?)?(?:工作|任务)|会话(?:主题|总结)|主题标题|待用户补充|询问任务进度|task (?:update|summary|progress)|project update)[。.!?？]*$`)
var bareTopicTools = regexp.MustCompile(`^(?:[A-Za-z][\w-]*_[\w-]+)(?:[,，、 ]+[A-Za-z][\w-]*_[\w-]+)*$`)

func weakTopic(text string) bool {
	return defaultTopicTitle(text) || topicFiller.MatchString(text) || genericTopicTitle.MatchString(text) || statusTopicPhrase.MatchString(text) || bareTopicTools.MatchString(text)
}

func managedTopic(s *Session) bool {
	// Old files have no provenance. Preserve a descriptive legacy title, which
	// may have been supplied explicitly before titleSource existed.
	return s != nil && s.Kind != assistantSessionKind && s.TitleSource != "manual" && (s.TitleSource == "auto" || weakTopic(s.Title))
}

func userTopicText(raw string) string {
	raw = strings.TrimSpace(raw)
	inFence := false
	for _, line := range strings.SplitN(raw, "\n", 9) {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || text == "" || strings.HasPrefix(text, ">") || strings.HasPrefix(text, "【系统") {
			continue
		}
		text = strings.TrimSpace(strings.TrimLeft(text, "#"))
		// A greeting on its own carries no topic; a greeting before a request can be discarded.
		for _, prefix := range []string{"好的，", "明白，", "谢谢，", "你好，", "您好，", "你好。", "您好。", "你好！", "您好！", "请你帮我", "麻烦你帮我", "麻烦帮我", "请帮我", "帮我", "我想让你", "我想要你"} {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
		if text == "" || weakTopic(text) {
			continue
		}
		return truncateRunes(strings.Join(strings.Fields(text), " "), 220)
	}
	return ""
}

var topicShift = regexp.MustCompile(`(?i)^(?:现在(?:改为|改做|转为|换成)|(?:换个(?:话题|任务)|新任务|接下来改做|切换到|改为研究|改做|转而)(?:[，,：: ]*)|switch(?: the topic)? to |instead[, ]+|new task[ :]+)`)

type sessionTopicIntent struct {
	first, previous, latest, focus string
	latestRun                      *Task
}

func topicIntent(s *Session) sessionTopicIntent {
	var intent sessionTopicIntent
	add := func(raw string, run *Task) {
		text := userTopicText(raw)
		if text == "" {
			return
		}
		if intent.first == "" {
			intent.first, intent.focus = text, text
		}
		if next := topicShift.ReplaceAllString(text, ""); next != text && strings.TrimSpace(next) != "" {
			intent.focus = strings.TrimSpace(next)
		}
		if text != intent.latest {
			intent.previous, intent.latest = intent.latest, text
		}
		intent.latestRun = run
	}
	for _, run := range s.Runs {
		if run == nil {
			continue
		}
		add(run.Prompt, run)
		for _, steer := range run.Steers {
			add(steer.Content, run)
		}
	}
	if intent.first == "" {
		for _, msg := range s.Messages {
			if msg.Role == "user" {
				add(msg.Content, nil)
			}
		}
	}
	return intent
}

// Some requests explicitly name several deliverables. Keep both objects in
// a short local label even if optional requirements between them are long.
var linkedTopicDeliverable = regexp.MustCompile(`(?:并|同时)(?:为其|为此|为它)?(?:生成|制作|编写|整理|导出)(?:对应|配套)?的?([^，,。；;]{2,26})$`)
var topicLeadIn = regexp.MustCompile(`^(?:请(?:先)?|麻烦)?(?:生成|制作|编写|整理)(?:一份|一个|一部|一套)`)
var topicWordCount = regexp.MustCompile(`([万亿千百0-9]+字|[一二三四五六七八九十]+[万亿千百]+字)的`)

var topicProcedure = regexp.MustCompile(`^([^。！？；]{2,10}(?:验收|核验|审查|验证|检查|测试|演练))[。！？；]\s*(?:请|先|再|然后|接着)*(?:使用|用|调用|通过|借助)\s*[A-Za-z][A-Za-z0-9_-]*\s*(?:工具|命令|接口)?[：:,， ]*([^，,。；;]+)`)
var topicGeneratedObject = regexp.MustCompile(`(?:下|里|中|内)的?\s*[^ ，。]*[-_][A-Fa-f0-9]{6,}\b.*$`)

func localTopicTitle(intent sessionTopicIntent) string {
	title := strings.TrimSpace(strings.TrimRight(intent.focus, "。？！，,.!? "))
	// In procedural requests the leading tool identifier describes the method.
	// Retain the requested operation and its object plus the user's own label.
	if parts := topicProcedure.FindStringSubmatch(title); parts != nil {
		action := strings.TrimSpace(topicGeneratedObject.ReplaceAllString(parts[2], ""))
		if action != "" {
			title = action + " · " + parts[1]
		}
	}
	if match := linkedTopicDeliverable.FindStringSubmatchIndex(title); match != nil {
		head := strings.FieldsFunc(title[:match[0]], func(r rune) bool { return strings.ContainsRune("，,。；;", r) })
		if len(head) > 0 {
			first := topicWordCount.ReplaceAllString(topicLeadIn.ReplaceAllString(head[0], ""), "${1}")
			last := title[match[2]:match[3]]
			if first != "" {
				title = first + "与" + last
			}
		}
	}
	if utf8.RuneCountInString(title) <= topicTitleLimit {
		return title
	}
	return truncateRunes(title, topicTitleLimit-1)
}

func displaySessionTitle(s *Session) string {
	if s == nil {
		return ""
	}
	if !managedTopic(s) || !weakTopic(s.Title) {
		return s.Title
	}
	if title := localTopicTitle(topicIntent(s)); title != "" {
		return title
	}
	if defaultTopicTitle(s.Title) && s.Title != "" {
		return s.Title
	}
	return "新会话"
}

func setLocalTopic(s *Session, prompt string) {
	if !managedTopic(s) {
		return
	}
	text := userTopicText(prompt)
	if text == "" {
		return
	}
	if weakTopic(s.Title) || topicShift.MatchString(text) {
		if title := localTopicTitle(topicIntent(s)); title != "" {
			s.Title, s.TitleSource = title, "auto"
		}
	}
}

func topicMatchesIntent(title string, intent sessionTopicIntent) bool {
	source := strings.ToLower(strings.Join([]string{intent.first, intent.previous, intent.latest}, "\n"))
	if intent.focus != intent.first {
		// A later explicit switch supersedes the original subject.
		source = strings.ToLower(intent.focus + "\n" + intent.latest)
	}
	// Retain an identifiable subject from the user's words; a fluent but
	// unrelated answer is a poor navigation label.
	var lastHan rune
	for _, word := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		lastHan = 0
		if len(word) > 1 && strings.Contains(source, word) {
			return true
		}
		for _, r := range word {
			if unicode.Is(unicode.Han, r) {
				if lastHan != 0 && strings.Contains(source, string([]rune{lastHan, r})) {
					return true
				}
				lastHan = r
			} else {
				lastHan = 0
			}
		}
	}
	return false
}

func (a *App) summarizeTopic(ctx context.Context, s *Session, task *Task, cfg Settings, params ProfileParams) {
	if mediaTask(cfg) {
		return
	}
	a.mu.Lock()
	if s.Deleted || !managedTopic(s) || userTopicText(task.Prompt) == "" {
		a.mu.Unlock()
		return
	}
	intent := topicIntent(s)
	if intent.latestRun != task {
		a.mu.Unlock()
		return
	}
	summaryParams := params
	summaryParams.MaxTokens = 64
	summaryParams.ResponseFormat = "text"
	current := s.Title
	contextText := "最初目标：" + intent.first
	if intent.focus != intent.first {
		contextText += "\n已切换的工作目标：" + intent.focus
	}
	if intent.previous != "" && intent.previous != intent.first {
		contextText += "\n此前补充：" + intent.previous
	}
	if intent.latest != intent.first {
		contextText += "\n最近目标或补充：" + intent.latest
	}
	input := []Message{
		{Role: "system", Content: "提炼一个可在会话列表独立辨认的主题短语。只输出具体对象和要做的事，优先项目名、作品名或明确问题。综合最初目标和后续补充，明确换题时以新目标为主。忽略寒暄、催促、工具名和进度播报。保留用户语言和专有名词，不回答问题，不添加事实；最多32个字符。"},
		{Role: "user", Content: contextText},
	}
	topicContext := requestContext(input, nil, summaryParams.MaxTokens, a.modelWindow(cfg.Model))
	task.TopicContext = topicContext
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if a.bgCtx != nil {
		stop := context.AfterFunc(a.bgCtx, cancel)
		defer stop()
	}
	topic, _, usage, err := complete(withoutKVCache(ctx), cfg, input, summaryParams, nil, nil)
	if err != nil {
		return
	}
	topic = strings.Trim(strings.TrimSpace(topic), "“”\"")
	a.mu.Lock()
	defer a.mu.Unlock()
	task.Usage = addUsage(task.Usage, usage)
	topicContext.Usage = usage
	if a.sessions[s.ID] != s || s.Deleted {
		return
	}
	oldTitle, oldSource := s.Title, s.TitleSource
	if s.Title == current && managedTopic(s) && intent == topicIntent(s) &&
		!strings.ContainsAny(topic, "\r\n`{}") && utf8.RuneCountInString(topic) <= topicTitleLimit &&
		!weakTopic(topic) && topicMatchesIntent(topic, intent) {
		s.Title, s.TitleSource = topic, "auto"
	}
	// The auxiliary request consumed tokens even if its label was rejected
	// or a later turn superseded it. Persist usage together with the title.
	if err := a.save(s); err != nil {
		s.Title, s.TitleSource = oldTitle, oldSource
		return
	}
	if oldTitle != s.Title {
		a.broadcastSessionsChanged(s.ID)
	}
}
