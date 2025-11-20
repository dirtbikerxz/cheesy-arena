package model

import (
	"encoding/json"
	"errors"
	"time"
)

// GameConfig stores the current game definition as JSON so it can be loaded without hardcoded game-specific logic.
type GameConfig struct {
	Id          int `db:"id"`
	Name        string
	Version     string
	Payload     string
	LastUpdated time.Time
}

// GetGameConfig returns the persisted game configuration or initializes the database with a default one.
func (database *Database) GetGameConfig() (*GameConfig, error) {
	configs, err := database.gameConfigTable.getAll()
	if err != nil {
		return nil, err
	}
	if len(configs) == 1 {
		return &configs[0], nil
	}

	defaultPayload, err := defaultGameConfigJson()
	if err != nil {
		return nil, err
	}
	config := GameConfig{
		Name:        "Custom Game",
		Version:     "1.0.0",
		Payload:     defaultPayload,
		LastUpdated: time.Now(),
	}
	if err := database.gameConfigTable.create(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// UpdateGameConfig saves the provided game definition JSON.
func (database *Database) UpdateGameConfig(config *GameConfig) error {
	config.LastUpdated = time.Now()
	return database.gameConfigTable.update(config)
}

// defaultGameConfigJson provides a starter configuration with empty panels and foul definitions.
func defaultGameConfigJson() (string, error) {
	defaultConfig := map[string]any{
		"name":    "Custom Game",
		"version": "1.0.0",
		"panels": []map[string]any{
			{"id": "red_near", "title": "Red Near", "widgets": []any{}},
			{"id": "red_far", "title": "Red Far", "widgets": []any{}},
			{"id": "blue_near", "title": "Blue Near", "widgets": []any{}},
			{"id": "blue_far", "title": "Blue Far", "widgets": []any{}},
			{"id": "referee", "title": "Referee", "widgets": []any{}},
			{"id": "head_ref", "title": "Head Referee", "widgets": []any{}},
		},
		"rules": map[string]any{
			"fouls": []map[string]any{
				{"id": "minor", "label": "Minor Foul", "points": 0},
				{"id": "major", "label": "Major Foul", "points": 0, "isMajor": true},
			},
		},
	}

	bytes, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		return "", errors.New("failed to marshal default configuration")
	}
	return string(bytes), nil
}
