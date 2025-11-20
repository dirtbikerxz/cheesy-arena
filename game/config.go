package game

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ActiveGameConfig holds the parsed game definition from the builder.
var ActiveGameConfig *GameConfigDefinition

type GameConfigDefinition struct {
	Name    string           `json:"name"`
	Version string           `json:"version"`
	Panels  []PanelConfig    `json:"panels"`
	Rules   RuleConfig       `json:"rules"`
	Scoring []ScoringElement `json:"scoring"`
}

type PanelConfig struct {
	Id      string         `json:"id"`
	Title   string         `json:"title"`
	Widgets []WidgetConfig `json:"widgets"`
}

type WidgetConfig struct {
	Id          string         `json:"id"`
	Type        string         `json:"type"`
	Label       string         `json:"label"`
	Points      string         `json:"points"`
	Phase       string         `json:"phase"`
	Color       string         `json:"color"`
	ScoringId   string         `json:"scoringId"`
	Position    GridPosition   `json:"position"`
	StatePoints map[string]int `json:"-"`
	PointValue  int            `json:"-"`
}

type RuleConfig struct {
	Fouls []FoulRule `json:"fouls"`
}

type FoulRule struct {
	Id      string `json:"id"`
	Label   string `json:"label"`
	Points  int    `json:"points"`
	IsMajor bool   `json:"isMajor"`
}

type ScoringElement struct {
	Id         string `json:"id"`
	Label      string `json:"label"`
	PointValue int    `json:"pointValue"`
	Phase      string `json:"phase"`
}

type GridPosition struct {
	Row     int `json:"row"`
	Col     int `json:"col"`
	ColSpan int `json:"colSpan"`
}

// SetActiveGameConfig parses the JSON payload and stores the result for runtime use.
func SetActiveGameConfig(payload string) error {
	definition, err := ParseGameConfig(payload)
	if err != nil {
		return err
	}
	ActiveGameConfig = definition
	return nil
}

// ParseGameConfig converts the builder JSON into a strongly typed runtime definition.
func ParseGameConfig(payload string) (*GameConfigDefinition, error) {
	var cfg GameConfigDefinition
	if err := json.Unmarshal([]byte(payload), &cfg); err != nil {
		return nil, err
	}
	for i := range cfg.Panels {
		for j := range cfg.Panels[i].Widgets {
			cfg.Panels[i].Widgets[j].PointValue, cfg.Panels[i].Widgets[j].StatePoints =
				parsePoints(cfg.Panels[i].Widgets[j].Points)
			if cfg.Panels[i].Widgets[j].Id == "" {
				cfg.Panels[i].Widgets[j].Id = fmt.Sprintf("%s_%d", cfg.Panels[i].Id, j)
			}
		}
	}
	return &cfg, nil
}

// WidgetById finds a widget from any panel.
func (cfg *GameConfigDefinition) WidgetById(id string) *WidgetConfig {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Panels {
		for j := range cfg.Panels[i].Widgets {
			if cfg.Panels[i].Widgets[j].Id == id {
				return &cfg.Panels[i].Widgets[j]
			}
		}
	}
	return nil
}

// PanelById finds a panel config by ID.
func (cfg *GameConfigDefinition) PanelById(id string) *PanelConfig {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Panels {
		if cfg.Panels[i].Id == id {
			return &cfg.Panels[i]
		}
	}
	return nil
}

func parsePoints(points string) (int, map[string]int) {
	points = strings.TrimSpace(points)
	statePoints := map[string]int{}
	if strings.Contains(points, ":") {
		parts := strings.Split(points, ",")
		for _, p := range parts {
			pair := strings.SplitN(strings.TrimSpace(p), ":", 2)
			if len(pair) != 2 {
				continue
			}
			val, err := strconv.Atoi(strings.TrimSpace(pair[1]))
			if err != nil {
				continue
			}
			statePoints[strings.TrimSpace(pair[0])] = val
		}
		return 0, statePoints
	}
	if points == "" {
		return 1, statePoints
	}
	val, err := strconv.Atoi(points)
	if err != nil {
		return 1, statePoints
	}
	return val, statePoints
}
