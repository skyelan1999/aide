package tts

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CatalogModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type QwenCatalog struct {
	Models      []CatalogModel `json:"models"`
	Voices      []Voice        `json:"voices"`
	ModelSource string         `json:"modelSource"`
	VoiceSource string         `json:"voiceSource"`
	Warning     string         `json:"warning,omitempty"`
}

// Preset IDs verified against the official non-realtime voice table, 2026-10-17.
// Names and identifiers only; no model output or copied descriptions.
func DefaultQwenCatalog() QwenCatalog {
	ids := strings.Split("Cherry,Serena,Ethan,Chelsie,Momo,Vivian,Moon,Maia,Kai,Nofish,Bella,Jennifer,Ryan,Katerina,Aiden,Eldric Sage,Mia,Mochi,Bellona,Vincent,Bunny,Neil,Elias,Arthur,Nini,Seren,Pip,Stella,Bodega,Sonrisa,Alek,Dolce,Sohee,Ono Anna,Lenn,Emilien,Andre,Radio Gol,Jada,Dylan,Li,Marcus,Roy,Peter,Sunny,Eric,Rocky,Kiki", ",")
	names := strings.Split("芊悦,苏瑶,晨煦,千雪,茉兔,十三,月白,四月,凯,不吃鱼,萌宝,詹妮弗,甜茶,卡捷琳娜,艾登,沧明子,乖小妹,沙小弥,燕铮莺,田叔,萌小姬,阿闻,墨讲师,徐大爷,邻家妹妹,小婉,顽屁小孩,少女阿月,博德加,索尼莎,阿列克,多尔切,素熙,小野杏,莱恩,埃米尔安,安德雷,拉迪奥·戈尔,上海-阿珍,北京-晓东,南京-老李,陕西-秦川,闽南-阿杰,天津-李彼得,四川-晴儿,四川-程川,粤语-阿强,粤语-阿清", ",")
	voices := make([]Voice, 0, len(ids))
	for i, id := range ids {
		voices = append(voices, Voice{ID: id, Name: names[i]})
	}
	return QwenCatalog{Models: []CatalogModel{{ID: "qwen3-tts-flash", Name: "千问3 TTS Flash"}}, Voices: voices, ModelSource: "builtin", VoiceSource: "builtin"}
}
func compatibleQwenModel(id string) bool {
	return (id == "qwen3-tts-flash" || strings.HasPrefix(id, "qwen3-tts-flash-")) && !strings.Contains(id, "realtime")
}

// DiscoverQwen reads catalogs only; it never synthesizes speech or changes settings.
func DiscoverQwen(ctx context.Context, cfg Config) QwenCatalog {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return discoverQwen(ctx, cfg, client)
}
func discoverQwen(ctx context.Context, cfg Config, client *http.Client) QwenCatalog {
	c := DefaultQwenCatalog()
	var warnings []string
	host := "dashscope.aliyuncs.com"
	if cfg.Region == "singapore" {
		host = "dashscope-intl.aliyuncs.com"
	} else if cfg.Region != "" && cfg.Region != "beijing" {
		c.Warning = "百炼地域无效，使用内置目录"
		return c
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		models := map[string]string{}
		ok := true
		for page := 1; page <= 10; page++ {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/api/v1/models?capabilities=TTS&page_size=100&page_no="+strconv.Itoa(page), nil)
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
			res, err := client.Do(req)
			if err != nil {
				warnings = append(warnings, "模型目录连接失败，使用内置目录")
				ok = false
				break
			}
			var data struct {
				Code   string `json:"code"`
				Output struct {
					Total  int `json:"total"`
					Models []struct {
						Model string `json:"model"`
						ID    string `json:"id"`
						Name  string `json:"name"`
					} `json:"models"`
				} `json:"output"`
			}
			err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&data)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 || data.Code != "" {
				warnings = append(warnings, fmt.Sprintf("模型目录 HTTP %d，请检查地域与密钥；使用内置目录", res.StatusCode))
				ok = false
				break
			}
			added := 0
			for _, m := range data.Output.Models {
				id := m.Model
				if id == "" {
					id = m.ID
				}
				if compatibleQwenModel(id) {
					if _, exists := models[id]; !exists {
						added++
					}
					models[id] = m.Name
				}
			}
			if len(data.Output.Models) == 0 || page*100 >= data.Output.Total || added == 0 && page > 1 {
				break
			}
		}
		if ok && len(models) > 0 {
			c.Models = nil
			for id, name := range models {
				c.Models = append(c.Models, CatalogModel{ID: id, Name: name})
			}
			sort.Slice(c.Models, func(i, j int) bool { return c.Models[i].ID < c.Models[j].ID })
			c.ModelSource = "api"
		} else if ok {
			warnings = append(warnings, "目录未返回当前语音协议兼容型号，使用内置目录")
		}
	} else {
		warnings = append(warnings, "尚未配置密钥，模型使用内置目录")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://help.aliyun.com/zh/model-studio/qwen-tts-voice-list", nil)
	if res, err := client.Do(req); err == nil {
		b, e := io.ReadAll(io.LimitReader(res.Body, 3<<20))
		res.Body.Close()
		if e == nil && res.StatusCode == 200 {
			if voices := parseQwenVoices(string(b)); len(voices) >= 4 {
				c.Voices = voices
				c.VoiceSource = "official"
			}
		}
	}
	if c.VoiceSource != "official" {
		warnings = append(warnings, "官方音色目录暂不可达，使用已内置的官方音色")
	}
	c.Warning = strings.Join(warnings, "；")
	return c
}

var qwenScripts = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(?:script|style)>`)
var qwenTags = regexp.MustCompile(`<[^>]*>`)
var qwenVoicePattern = regexp.MustCompile(`名称\s*[：:]\s*(.*?)\s*voice\s*参数\s*[：:]\s*([A-Za-z][A-Za-z ]{0,40}?)\s*描述\s*[：:]`)

func parseQwenVoices(page string) []Voice {
	// The help portal embeds the article as JSON. Decode data, never execute scripts.
	if at := strings.Index(page, "window.__ICE_PAGE_PROPS__="); at >= 0 {
		var data struct {
			DocDetailData struct {
				StoreData struct {
					Data struct {
						Content string `json:"content"`
					} `json:"data"`
				} `json:"storeData"`
			} `json:"docDetailData"`
		}
		if err := json.NewDecoder(strings.NewReader(page[at+len("window.__ICE_PAGE_PROPS__="):])).Decode(&data); err == nil && data.DocDetailData.StoreData.Data.Content != "" {
			page = data.DocDetailData.StoreData.Data.Content
		}
	}

	text := html.UnescapeString(qwenTags.ReplaceAllString(qwenScripts.ReplaceAllString(page, " "), " "))
	if at := strings.Index(text, "非实时语音合成"); at >= 0 {
		text = text[at:]
	}
	out := []Voice{}
	seen := map[string]bool{}
	for _, m := range qwenVoicePattern.FindAllStringSubmatch(text, -1) {
		id := strings.TrimSpace(m[2])
		name := strings.TrimSpace(m[1])
		if len(name) > 120 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Voice{ID: id, Name: name})
	}
	return out
}
