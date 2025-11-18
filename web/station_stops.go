package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

type stationStopRequest struct {
	EStop  bool   `json:"eStop"`
	AStop  bool   `json:"aStop"`
	Secret string `json:"secret"`
}

func (web *Web) stationStopsApiHandler(w http.ResponseWriter, r *http.Request) {
	if web.arena.EventSettings == nil || !web.arena.EventSettings.UseStationRpiStops {
		http.Error(w, "station RPi stops disabled", http.StatusServiceUnavailable)
		return
	}

	var req stationStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	secret := strings.TrimSpace(req.Secret)
	if web.arena.EventSettings.StationRpiSecret != "" {
		if secret == "" {
			http.Error(w, "secret required", http.StatusForbidden)
			return
		}
		if secret != web.arena.EventSettings.StationRpiSecret {
			http.Error(w, "invalid secret", http.StatusForbidden)
			return
		}
	}

	station := strings.ToUpper(r.PathValue("stationId"))
	if err := web.arena.UpdateRemoteStops(station, req.EStop, req.AStop); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}
