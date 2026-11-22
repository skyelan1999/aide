package server

import (
	"context"
	"regexp"
	"strings"
)

type assistantCallHistoryKey struct{}

// Only actual earlier user turns from this socket are reference context. The
// current utterance remains the final user message and sole new authorization.
func nativeCallWorkHistory(order []string, heard map[string]string, current string) []Message {
	result := []Message{}
	for _, id := range order {
		if id == current {
			break
		}
		text := strings.TrimSpace(heard[id])
		lower := strings.ToLower(text)
		if text == "" || strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "api key") || strings.Contains(text, "密码") || strings.Contains(text, "密钥") || strings.Contains(text, "口令") {
			continue
		}
		result = append(result, Message{Role: "user", Content: boundedEvidenceText(text, 300)})
	}
	if len(result) > 6 {
		result = result[len(result)-6:]
	}
	return result
}
func assistantCurrentCallHistory(ctx context.Context) []Message {
	history, _ := ctx.Value(assistantCallHistoryKey{}).([]Message)
	return history
}

var nativeWeatherFact = regexp.MustCompile(`气温|温度|湿度|降水概率|[零一二三四五六七八九十百0-9]+\s*(?:℃|°C|摄氏度|度)|(?:今天|明天|后天|下周|未来|本周|天气).{0,20}(?:晴|多云|阴天|下雨|降雨|有雨|有雪|降雪|微风)`)

func nativeWeatherClaim(text string) bool { return nativeWeatherFact.MatchString(text) }

var nativeOpeningSmallTalk = regexp.MustCompile(`(?i)^(?:hello|hi|hey|yeah|ok|okay|哈[喽啰罗]|你[好号]|嗯|哦|喔|啊|好|好的|行|可以|对|是|是的|现在|继续|再来|什么话题)[\s,.!，。！?？…]*$`)

func nativeOpeningTopic(text string) bool {
	return len([]rune(strings.TrimSpace(text))) >= 4 && !nativeOpeningSmallTalk.MatchString(text)
}
