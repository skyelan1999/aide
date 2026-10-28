package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeSpeechReviewHoldsSpeculativeAudioAndBoundsBuffer(t *testing.T) {
	g := &nativeSpeechReview{enabled: true}
	e := map[string]json.RawMessage{"type": json.RawMessage(`"response.audio.delta"`)}
	if !g.hold(e, 640) || len(g.events) != 1 {
		t.Fatal("speculative audio escaped before review")
	}
	if !g.hold(e, 480000) || !g.rejected || len(g.events) != 0 {
		t.Fatal("unbounded speech buffer")
	}
	if !g.hold(e, 640) || len(g.events) != 0 {
		t.Fatal("rejected audio retained")
	}
	normal := &nativeSpeechReview{}
	if normal.hold(e, 640) {
		t.Fatal("ordinary speech stopped streaming")
	}
}

func TestNativeSpeechAssertionsKeepQuotedEvidenceDistinct(t *testing.T) {
	for _, text := range []string{"努力很久没结果确实让人沮丧", "这种迷茫感很正常", "你肯定很焦虑", "我完全懂你的感受", "确实让人心里堵得慌", "评审延期确实让人心里没底", "咱们先别慌", "允许自己难过一会儿"} {
		if !speculativeSpeechNeedsRevision(text) {
			t.Fatal("unqualified hypothesis accepted")
		}
	}
	for _, text := range []string{"我猜可能有点泄气，也可能不是这样", "先把最卡的一步写下来", "如果难过，可以先休息", "当时你自述的心情是焦虑", "原话是“你肯定很焦虑”，但那只是推测"} {
		if speculativeSpeechNeedsRevision(text) {
			t.Fatal("qualified speech or quotation rejected")
		}
	}
}

func TestNativeKnownResponseConflictDoesNotHideUnknownProviderErrors(t *testing.T) {
	if !nativeResponseAlreadyActive("invalid_request_error", "Conversation already has an active response") {
		t.Fatal("known native VAD/tool race not classified")
	}
	for _, pair := range [][2]string{{"", ""}, {"server_error", "Conversation already has an active response"}, {"invalid_request_error", "Quota exceeded"}, {"invalid_request_error", "Conversation already has an active response and account disabled"}} {
		if nativeResponseAlreadyActive(pair[0], pair[1]) {
			t.Fatal("unknown provider error suppressed")
		}
	}
}

func TestNativeProviderDiagnosticIsPrivateBoundedAndRedacted(t *testing.T) {
	dir := t.TempDir()
	nativeProviderDiagnostic(dir, "invalid_request_error", "", "bad private-key at https://example.com/secret and abcdefghijklmnopqrstuvwxyz", "private-key")
	p := filepath.Join(dir, "assistant", "mixsoul", "native-provider-diagnostic.jsonl")
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"private-key", "example.com", "abcdefghijklmnopqrstuvwxyz"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("diagnostic leaked secret")
		}
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("diagnostic not private")
	}
	if os.WriteFile(p, make([]byte, 65536), 0600) != nil {
		t.Fatal("fixture")
	}
	nativeProviderDiagnostic(dir, "x", "x", "x")
	info, _ = os.Stat(p)
	if info.Size() != 65536 {
		t.Fatal("unbounded diagnostic")
	}
}

func TestNativeCalibratedSpeechIsBoundedAndQuotesCurrentUtterance(t *testing.T) {
	p := nativeCalibratedSpeechInstructions("assistant", "项目卡住了\n\"旧回答\"")
	if !strings.Contains(p, "最多25个汉字") || !strings.Contains(p, `\n\"旧回答\"`) {
		t.Fatal("current utterance was not bounded as data")
	}
	if len([]rune(nativeCalibratedSpeechInstructions("assistant", strings.Repeat("测", 5000)))) > 1600 {
		t.Fatal("retry context unbounded")
	}
}

func TestNativeEmotionEvidenceDenialDoesNotBecomeAssertion(t *testing.T) {
	for _, text := range []string{
		"你只说不知下一步，没提焦虑。", "你未明确说过焦虑。", "记录中没确认你当时焦虑。",
		"不能肯定你的心情，也不一定是焦虑。", "你没有说过焦虑，这是我的推测。",
	} {
		if speculativeSpeechNeedsRevision(text) {
			t.Fatalf("negated evidence blocked: %s", text)
		}
	}
	for _, text := range []string{
		"没提焦虑，但你很焦虑。", "你没有说过焦虑，你肯定很焦虑。",
		"不能肯定细节，但你一定很难过。", "没有结果，你心里没底。",
	} {
		if !speculativeSpeechNeedsRevision(text) {
			t.Fatalf("unsupported assertion escaped: %s", text)
		}
	}
}
