package server

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestNativeClockRefreshWaitsForMicrophoneAndPlaybackIdle(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Minute)
	quiet := now.Add(-10 * time.Second)
	if !nativeClockRefreshDue(now, old, quiet, false, false, quiet) {
		t.Fatal("quiet clock never refreshed")
	}
	for _, row := range []struct {
		lastInput          time.Time
		speaking, replying bool
		playback           time.Time
	}{{now, false, false, quiet}, {quiet, true, false, quiet}, {quiet, false, true, quiet}, {quiet, false, false, now.Add(time.Second)}} {
		if nativeClockRefreshDue(now, old, row.lastInput, row.speaking, row.replying, row.playback) {
			t.Fatal("time update could interrupt current audio")
		}
	}
	if nativeClockRefreshDue(now, quiet, quiet, false, false, quiet) {
		t.Fatal("clock refreshed too often")
	}
	pcm := make([]byte, 640)
	if nativePCMHasSpeechEnergy(pcm) {
		t.Fatal("digital silence treated as speech")
	}
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 1024)
	}
	if !nativePCMHasSpeechEnergy(pcm) {
		t.Fatal("actual microphone activity not detected")
	}
}

func TestNativeVADModeAndASRUseSupportedProviderContract(t *testing.T) {
	for _, row := range []struct{ input, want string }{{"", "semantic_vad"}, {"invalid", "semantic_vad"}, {"semantic_vad_ec", "semantic_vad_ec"}, {"server_vad", "server_vad"}} {
		t.Setenv("AIDE_NATIVE_VAD_MODE", row.input)
		session := realtimeCallSession(Settings{})
		if session["turn_detection"].(map[string]any)["type"] != row.want {
			t.Fatal("unvalidated native VAD")
		}
		if session["input_audio_transcription"].(map[string]any)["model"] != "qwen3-asr-flash-realtime" {
			t.Fatal("legacy ASR on Qwen3.8")
		}
	}
}
