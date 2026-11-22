package server

import (
	"regexp"
	"strings"
)

// This only requests one model/tool retry. It is never authority to dispatch
// raw input, approve a task, or bypass a missing/failed model.
var assistantWorkMeta = regexp.MustCompile(`^(?:请问|你知道)?(?:怎么|如何).{0,12}(?:查|使用|开发)|(?:查询|文件处理|天气查询)的(?:原理|功能)|是否具备|(?:功能|插件)怎么用`)
var assistantWorkClauses = regexp.MustCompile(`[，,。；;！!？?\n]+`)
var assistantWorkNegation = regexp.MustCompile(`^(?:请)?(?:不要|不用|别|无需|不需要|不动|不改|不批准|不处理|保留)`)

var assistantExternalWork = regexp.MustCompile(`(?:今天|明天|后天|现在|这[里儿]|当地|目前|下下周|下周|本周|这周|最近一周|未来.{0,4}天).{0,12}(?:天气|气温|下雨|降雪|有雨|有雪)|(?:查|看|读|分析|总结|概括|解释|解读).{0,100}(?:文件|文档|资料|指南|README|readme|PDF|pdf|\.md|\.txt|\.docx|天气|气温)|(?:文件|文档|指南|PDF|pdf).{0,60}(?:讲什么|内容|总结|概括)|(?:帮我|请).{0,30}(?:查询|检索|写代码|修改文件|运行测试|创建文件)`)

func assistantNeedsWorkTool(heard string) bool {
	text := strings.TrimSpace(heard)
	if text == "" || len(text) > 2000 || assistantApprovalStatusQuery(text) {
		return false
	}
	for _, no := range []string{"假如", "假设", "如果我说", "比如说", "只聊"} {
		if strings.Contains(text, no) {
			return false
		}
	}
	if sipCallbackUtterance.MatchString(text) {
		return true
	}
	if assistantWorkMeta.MatchString(text) {
		return false
	}
	for _, clause := range assistantWorkClauses.Split(text, -1) {
		clause = strings.TrimSpace(clause)
		if !assistantWorkNegation.MatchString(clause) && assistantExternalWork.MatchString(clause) {
			return true
		}
	}
	return false
}

const assistantAutonomyGuidance = `天气工具可用时直接调用 weather_forecast，当场回答，不创建执行会话；城市重名用 weather_search_locations。用户明确要查当前/这里天气可用 useNetworkLocation=true，但须说明网络出口可能受 VPN/代理影响。禁止索要模型密钥或静默请求系统定位权限。只有天气工具缺失才转交 Aide。用户要求查询实时信息、读取或解读工作区文件、分析资料、编写或执行工作，本身就是交办授权，不需要另外说“转交”“去调用 aide”或反复确认。能力不够时现在调用 dispatch_to_aide 或 push_to_session 建立并启动 Aide 会话，包含本轮对象、日期、路径和约束；相关路径可让执行会话在当前工作区查找，不让用户重新粘贴现有文件。工具失败或被禁用如实报告，不能冒充已启动。只有缺失无法合理查明的必要信息才问一次。闲聊、假设和背景声音不交办。交办沿现有权限执行，不批准已有待审任务，不自行扩大审批权限。工作结果会推送回助理会话；普通 SIP 电话已开启任务完成回拨时，宿主会关联本次新任务，先挂断后等待实际完成；只按回拨工具回执说明状态。不把旧任务或待审批说成完成，不承诺未登记的回拨。`
const assistantWorkToolRepair = `本轮用户已要求实际查询或处理工作，尚未取得执行回执。请使用当前可用工具查询，能力不足则调用 dispatch_to_aide 或 push_to_session 新建并启动 Aide 会话。不要只口头答应，不要要求用户再次确认交办或粘贴工作区现有文件。保持原对象及约束；若工具不可用则如实说明。`
