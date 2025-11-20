package field

import "github.com/Team254/cheesy-arena/game"

// LoadGameConfig parses the saved game configuration and applies it to runtime scoring.
func (arena *Arena) LoadGameConfig() error {
	config, err := arena.Database.GetGameConfig()
	if err != nil {
		return err
	}
	return game.SetActiveGameConfig(config.Payload)
}
