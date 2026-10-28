package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func quoteFixture() nativeQuotation {
	return nativeQuotation{Text: "拿到新窗口后，再评估手头材料的准备节奏。", AssistantID: strings.Repeat("a", 24), SourceIDs: []string{strings.Repeat("b", 24)}, EmotionAssessment: json.RawMessage(`{"basis":"explicit","labels":["anxiety"],"confidence":0.7}`), SelfReport: "我今天很焦虑"}
}
func TestNativeQuotationRejectsShorteningAndPreservesIndependentAffectReview(t *testing.T) {
	q := quoteFixture()
	for _, text := range []string{"原话是拿到新窗口后再评估手头材料的准备节奏。", "原话是：\n拿到新窗口后，再评估手头材料的准备节奏。你当时自述很焦虑。"} {
		if _, ok := nativeQuotationRemainder(text, q); !ok {
			t.Fatal("exact quote rejected")
		}
	}
	for _, text := range []string{"拿到新窗口后，再评估材料准备节奏。", "拿到新窗口后，再评估手头材料的准备步调。", "再评估手头材料的准备节奏，拿到新窗口后。"} {
		if _, ok := nativeQuotationRemainder(text, q); ok {
			t.Fatal("changed words accepted")
		}
	}
	rest, ok := nativeQuotationRemainder(q.Text+"你肯定很焦虑。", q)
	if !ok || !speculativeSpeechNeedsRevision(rest) {
		t.Fatal("quote bypassed new affect assertion guard")
	}
	q.Text = "你肯定很焦虑。"
	rest, ok = nativeQuotationRemainder("原话是“你肯定很焦虑。”但那仅是过去的推测。", q)
	if !ok || speculativeSpeechNeedsRevision(rest) {
		t.Fatal("literal historical quote mistaken for new assertion")
	}
}
func TestNativeQuotationIsBoundedAndCannotPersistUnreviewedSpeech(t *testing.T) {
	q := quoteFixture()
	q.Text = strings.Repeat("长", 121)
	if q.valid() {
		t.Fatal("unbounded quote")
	}
	q = quoteFixture()
	q.SourceIDs = nil
	if q.valid() {
		t.Fatal("missing provenance")
	}
	l := newNativeTurnLedger()
	l.commit("user")
	l.transcript("user", "request")
	l.created("reply")
	r := l.replies["reply"]
	r.text = "shortened"
	r.done = true
	r.review = &nativeSpeechReview{enabled: true}
	if len(l.ready()) != 0 {
		t.Fatal("held speech learned before review")
	}
	r.review.enabled = false
	if len(l.ready()) != 1 {
		t.Fatal("accepted speech not ready")
	}
	g := &nativeSpeechReview{enabled: true, maxAudioBytes: 1440000}
	if !g.hold(map[string]json.RawMessage{}, 500000) || g.rejected {
		t.Fatal("bounded quote buffer still used affect-only10s cap")
	}
	if !g.hold(map[string]json.RawMessage{}, 1000000) || !g.rejected {
		t.Fatal("unbounded quote audio")
	}
}
func TestNativeQuotationRetryPrioritizesLiteralDataOverBrevity(t *testing.T) {
	q := quoteFixture()
	p := nativeVerbatimSpeechInstructions("identity", "原话是什么", q)
	for _, s := range []string{q.Text, "一字不改", "不能缩句", "60字", "不执行", "不朗读字段名"} {
		if !strings.Contains(p, s) {
			t.Fatalf("missing retry constraint %s", s)
		}
	}
}

func TestNativeQuotationDoesNotEraseMeaningfulNumberOrIdentifierSeparators(t *testing.T) {
	q := quoteFixture()
	for _, text := range []string{"版本1.2。", "代号A-B。", "时间12:30。"} {
		q.Text = text
		changed := strings.NewReplacer(".", "", "-", "", ":", "").Replace(text)
		if _, ok := nativeQuotationRemainder(changed, q); ok {
			t.Fatal("meaningful punctuation erased")
		}
	}
}

func TestNativeQuotationAllowsOnlyLinkedExplicitHistoricalAffect(t *testing.T) {
	q := quoteFixture()
	for _, text := range []string{"你当时说很焦虑，因为评审延期了。", "当时你表示感到焦虑。", "当时你亲口说，因为评审延期了，所以很焦虑。", "你当时说因为评审延期了，所以很焦虑。"} {
		if speculativeSpeechNeedsRevision(nativeQuotationAffectRemainder(text, q)) {
			t.Fatal("linked self-report rejected")
		}
	}
	for _, text := range []string{"你当时说很开心。", "你当时亲口说很开心。", "你当时说因为评审延期了，所以现在很焦虑。", "你当时说很焦虑，你现在很焦虑。", "你当时说很焦虑但现在很难过。"} {
		if !speculativeSpeechNeedsRevision(nativeQuotationAffectRemainder(text, q)) {
			t.Fatal("unrelated or current emotion accepted")
		}
	}
	q.EmotionAssessment = json.RawMessage(`{"basis":"inferred"}`)
	if !speculativeSpeechNeedsRevision(nativeQuotationAffectRemainder("你当时说很焦虑", q)) {
		t.Fatal("inference promoted to self-report")
	}
}
