package server

import (
	"encoding/binary"
	"os"
	"time"
)

// This is only a conservative configuration-update gate, not the provider VAD.
// Browser capture keeps streaming quiet PCM while no-one speaks.
func nativePCMHasSpeechEnergy(pcm []byte) bool {
	if len(pcm) < 2 {
		return false
	}
	var sum int64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		if v < 0 {
			v = -v
		}
		sum += v
	}
	return sum > int64(len(pcm)/2)*128
}
func nativeClockRefreshDue(now, last, lastInput time.Time, speaking, replying bool, playbackUntil time.Time) bool {
	return now.Sub(last) >= time.Minute && !speaking && !replying && now.Sub(lastInput) > 5*time.Second && !now.Before(playbackUntil.Add(3*time.Second))
}

// Operator-only local acceptance tuning; never accepts arbitrary provider fields.
func nativeVADMode() string {
	switch os.Getenv("AIDE_NATIVE_VAD_MODE") {
	case "semantic_vad_ec":
		return "semantic_vad_ec"
	case "server_vad":
		return "server_vad"
	default:
		return "semantic_vad"
	}
}
