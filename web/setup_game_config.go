package web

import (
	"encoding/json"
	"fmt"
	htmlTemplate "html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Team254/cheesy-arena/model"
)

const gameConfigPath = "game/config.json"

// Renders the game configuration builder.
func (web *Web) gameConfigGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	config, err := web.arena.Database.GetGameConfig()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	tmpl, err := web.parseFiles("templates/setup_game_config.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}

	data := struct {
		EventSettings *model.EventSettings
		ConfigJson    htmlTemplate.JS
		ConfigName    string
		ConfigVersion string
		ErrorMessage  string
	}{
		web.arena.EventSettings,
		htmlTemplate.JS(config.Payload),
		config.Name,
		config.Version,
		"",
	}

	if err = tmpl.ExecuteTemplate(w, "base", data); err != nil {
		handleWebErr(w, err)
		return
	}
}

// Saves the posted game configuration JSON locally so it can be exported/loaded later.
func (web *Web) gameConfigPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	configJson := r.PostFormValue("config")
	if configJson == "" {
		web.renderGameConfigWithError(w, "No configuration payload provided.")
		return
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(configJson), &raw); err != nil {
		web.renderGameConfigWithError(w, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	config, err := web.arena.Database.GetGameConfig()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	if name, ok := raw["name"].(string); ok && name != "" {
		config.Name = name
	}
	if version, ok := raw["version"].(string); ok && version != "" {
		config.Version = version
	}
	config.Payload = configJson

	if err := web.arena.Database.UpdateGameConfig(config); err != nil {
		handleWebErr(w, err)
		return
	}

	_ = web.arena.LoadGameConfig()

	if err := os.MkdirAll(filepath.Dir(gameConfigPath), 0755); err == nil {
		_ = os.WriteFile(gameConfigPath, []byte(configJson), 0644)
	}

	http.Redirect(w, r, "/setup/game_config", http.StatusSeeOther)
}

func (web *Web) renderGameConfigWithError(w http.ResponseWriter, message string) {
	config, err := web.arena.Database.GetGameConfig()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	tmpl, err := web.parseFiles("templates/setup_game_config.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}

	data := struct {
		EventSettings *model.EventSettings
		ConfigJson    htmlTemplate.JS
		ConfigName    string
		ConfigVersion string
		ErrorMessage  string
	}{
		web.arena.EventSettings,
		htmlTemplate.JS(config.Payload),
		config.Name,
		config.Version,
		message,
	}

	_ = tmpl.ExecuteTemplate(w, "base", data)
}
