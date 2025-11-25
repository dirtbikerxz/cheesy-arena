// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web handlers for scoring interface.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	"github.com/mitchellh/mapstructure"
	"io"
	"log"
	"net/http"
	"strings"
)

// Renders the scoring interface which enables input of scores in real-time.
func (web *Web) scoringPanelHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	position := r.PathValue("position")
	var panelConfig *game.PanelConfig
	if game.ActiveGameConfig != nil {
		panelConfig = game.ActiveGameConfig.PanelById(position)
	}
	if panelConfig == nil {
		panelConfig = &game.PanelConfig{Id: position, Title: position}
	}
	alliance := strings.Split(position, "_")[0]

	template, err := web.parseFiles("templates/scoring_panel.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		PlcIsEnabled  bool
		PositionName  string
		Panel         *game.PanelConfig
		Alliance      string
		ScoringPoints map[string]int
	}{
		web.arena.EventSettings,
		web.arena.Plc.IsEnabled(),
		position,
		panelConfig,
		alliance,
		buildScoringPointMap(),
	}
	err = template.ExecuteTemplate(w, "base_no_navbar", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

func buildScoringPointMap() map[string]int {
	points := map[string]int{}
	if game.ActiveGameConfig == nil {
		return points
	}
	for _, s := range game.ActiveGameConfig.Scoring {
		points[s.Id] = s.PointValue
	}
	return points
}

// The websocket endpoint for the scoring interface client to send control commands and receive status updates.
func (web *Web) scoringPanelWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	position := r.PathValue("position")
	if game.ActiveGameConfig == nil || game.ActiveGameConfig.PanelById(position) == nil {
		handleWebErr(w, fmt.Errorf("Invalid position '%s'.", position))
		return
	}
	alliance := strings.Split(position, "_")[0]

	var realtimeScore **field.RealtimeScore
	if alliance == "red" {
		realtimeScore = &web.arena.RedRealtimeScore
	} else {
		realtimeScore = &web.arena.BlueRealtimeScore
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer ws.Close()
	web.arena.ScoringPanelRegistry.RegisterPanel(position, ws)
	web.arena.ScoringStatusNotifier.Notify()
	defer web.arena.ScoringStatusNotifier.Notify()
	defer web.arena.ScoringPanelRegistry.UnregisterPanel(position, ws)

	// Instruct panel to clear any local state in case this is a reconnect
	ws.Write("resetLocalState", nil)

	// Subscribe the websocket to the notifiers whose messages will be passed on to the client, in a separate goroutine.
	go ws.HandleNotifiers(
		web.arena.MatchLoadNotifier,
		web.arena.MatchTimeNotifier,
		web.arena.RealtimeScoreNotifier,
		web.arena.ReloadDisplaysNotifier,
	)

	// Loop, waiting for commands and responding to them, until the client closes the connection.
	for {
		command, data, err := ws.Read()
		if err != nil {
			if err == io.EOF {
				// Client has closed the connection; nothing to do here.
				return
			}
			log.Println(err)
			return
		}
		score := &(*realtimeScore).CurrentScore
		if score.GenericCounters == nil {
			score.GenericCounters = map[string]int{}
		}
		if score.GenericToggles == nil {
			score.GenericToggles = map[string]bool{}
		}
		if score.GenericStates == nil {
			score.GenericStates = map[string]string{}
		}
		scoreChanged := false

		if command == "commitMatch" {
			if web.arena.MatchState != field.PostMatch {
				// Don't allow committing the score until the match is over.
				ws.WriteError("Cannot commit score: Match is not over.")
				continue
			}
			web.arena.ScoringPanelRegistry.SetScoreCommitted(position, ws)
			web.arena.ScoringStatusNotifier.Notify()
		} else if command == "addFoul" {
			args := struct {
				Alliance string
				IsMajor  bool
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}

			// Add the foul to the correct alliance's list.
			foul := game.Foul{IsMajor: args.IsMajor}
			if args.Alliance == "red" {
				web.arena.RedRealtimeScore.CurrentScore.Fouls =
					append(web.arena.RedRealtimeScore.CurrentScore.Fouls, foul)
			} else {
				web.arena.BlueRealtimeScore.CurrentScore.Fouls =
					append(web.arena.BlueRealtimeScore.CurrentScore.Fouls, foul)
			}
			web.arena.RealtimeScoreNotifier.Notify()
		} else if command == "widget" {
			args := struct {
				WidgetId string
				Action   string
				Delta    int
				State    string
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			widget := game.ActiveGameConfig.WidgetById(args.WidgetId)
			if widget == nil {
				ws.WriteError(fmt.Sprintf("Unknown widget '%s'", args.WidgetId))
				continue
			}
			switch widget.Type {
			case "counter":
				if args.Delta == 0 {
					args.Delta = 1
				}
				score.GenericCounters[widget.Id] = max(0, score.GenericCounters[widget.Id]+args.Delta)
				scoreChanged = true
			case "toggle":
				score.GenericToggles[widget.Id] = !score.GenericToggles[widget.Id]
				scoreChanged = true
			case "multistate":
				if args.State != "" {
					score.GenericStates[widget.Id] = args.State
				} else {
					delete(score.GenericStates, widget.Id)
				}
				scoreChanged = true
			}
		}

		if scoreChanged {
			web.arena.RealtimeScoreNotifier.Notify()
		}
	}
}
