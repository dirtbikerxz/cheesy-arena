package game

// summarizeFromConfig produces a score summary based on the active configurable game settings.
func (score *Score) summarizeFromConfig(opponentScore *Score) *ScoreSummary {
	summary := new(ScoreSummary)
	if score.PlayoffDq {
		return summary
	}

	// Initialize maps if nil to avoid panics.
	if score.GenericCounters == nil {
		score.GenericCounters = map[string]int{}
	}
	if score.GenericToggles == nil {
		score.GenericToggles = map[string]bool{}
	}
	if score.GenericStates == nil {
		score.GenericStates = map[string]string{}
	}
	if score.GenericScoring == nil {
		score.GenericScoring = map[string]int{}
	}

	// Calculate points from generic widgets.
	for widgetId, value := range score.GenericCounters {
		if widget := ActiveGameConfig.WidgetById(widgetId); widget != nil {
			if widget.ScoringId != "" {
				score.GenericScoring[widget.ScoringId] += value
			} else {
				summary.MatchPoints += value * widget.PointValue
			}
		}
	}

	for widgetId, value := range score.GenericToggles {
		if !value {
			continue
		}
		if widget := ActiveGameConfig.WidgetById(widgetId); widget != nil {
			if widget.ScoringId != "" {
				score.GenericScoring[widget.ScoringId]++
			} else {
				summary.MatchPoints += widget.PointValue
			}
		}
	}

	for widgetId, state := range score.GenericStates {
		if widget := ActiveGameConfig.WidgetById(widgetId); widget != nil {
			if points, ok := widget.StatePoints[state]; ok {
				if widget.ScoringId != "" {
					score.GenericScoring[widget.ScoringId]++
					summary.MatchPoints += points
				} else {
					summary.MatchPoints += points
				}
			}
		}
	}

	// Apply scoring element point values.
	for _, scoring := range ActiveGameConfig.Scoring {
		count := score.GenericScoring[scoring.Id]
		summary.MatchPoints += count * scoring.PointValue
	}

	// Fouls assessed by the opponent.
	for _, foul := range opponentScore.Fouls {
		summary.FoulPoints += foul.PointValue()
		if foul.IsMajor {
			summary.NumOpponentMajorFouls++
		}
	}

	summary.Score = summary.MatchPoints + summary.FoulPoints
	return summary
}
