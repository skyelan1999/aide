package server

import (
	"aide/internal/server/tts"
	"context"
	"errors"
	"net/http"
	"time"
)

func (a *App) ttsCatalog(w http.ResponseWriter, r *http.Request) {
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	if r.Method == http.MethodGet {
		jsonOut(w, 200, tts.DefaultQwenCatalog())
		return
	}
	var in struct {
		Region string `json:"region"`
		APIKey string `json:"apiKey"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	cfg := tts.Config{Region: a.settings.TTSRegion, APIKey: a.settings.TTSAPIKey}
	a.mu.Unlock()
	if in.Region != "" {
		cfg.Region = in.Region
	}
	if in.APIKey != "" {
		cfg.APIKey = in.APIKey
	}
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	catalog := tts.DiscoverQwen(ctx, cfg)
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	jsonOut(w, 200, catalog)
}
