package server

import (
	"aide/internal/server/tts"
	"errors"
	"strings"
)

func preserveOpenAITTS(in *Settings, saved Settings, clear bool) error {
	if in.OpenAITTSMode == "" {
		in.OpenAITTSMode = saved.OpenAITTSMode
	}
	if in.OpenAITTSModel == "" {
		in.OpenAITTSModel = saved.OpenAITTSModel
	}
	if in.OpenAITTSVoice == "" {
		in.OpenAITTSVoice = saved.OpenAITTSVoice
	}
	if in.OpenAITTSBaseURL == "" {
		in.OpenAITTSBaseURL = saved.OpenAITTSBaseURL
	}
	if clear {
		in.OpenAITTSAPIKey = ""
	} else if strings.TrimSpace(in.OpenAITTSAPIKey) == "" {
		in.OpenAITTSAPIKey = saved.OpenAITTSAPIKey
	}
	in.OpenAITTSAPIKey = strings.TrimSpace(in.OpenAITTSAPIKey)
	if in.OpenAITTSMode != "" && in.OpenAITTSMode != "realtime" && in.OpenAITTSMode != "speech" {
		return errors.New("OpenAI 语音接口须为 realtime 或 speech")
	}
	_, err := tts.OpenAITTSBaseURL(in.OpenAITTSBaseURL)
	return err
}
