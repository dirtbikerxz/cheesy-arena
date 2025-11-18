package web

import (
	"encoding/json"
	"log"
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
	oldStatuses := web.arena.StationRpiStatuses()
	if err := web.arena.UpdateRemoteStops(station, req.EStop, req.AStop); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newStatuses := web.arena.StationRpiStatuses()
	oldStatus := oldStatuses[station]
	newStatus := newStatuses[station]
	if oldStatus.Online != newStatus.Online {
		log.Printf("Station RPi %s online=%t", station, newStatus.Online)
	}
	if oldStatus.RemoteEStop != newStatus.RemoteEStop || oldStatus.RemoteAStop != newStatus.RemoteAStop {
		log.Printf("Station RPi %s remote E=%t A=%t", station, newStatus.RemoteEStop, newStatus.RemoteAStop)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}
