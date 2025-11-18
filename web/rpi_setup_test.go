package web

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBuildStationRpiStatusView(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.UseStationRpiStops = true
	now := time.Now()
	web.arena.AllianceStations["R1"].RemoteLastUpdate = now
	web.arena.AllianceStations["R1"].RemoteEStop = true
	view := web.buildStationRpiStatusView()
	if assert.Equal(t, 6, len(view)) {
		assert.Equal(t, "R1", view[0].Station)
		assert.True(t, view[0].Online)
		assert.True(t, view[0].RemoteEStop)
	}
}
