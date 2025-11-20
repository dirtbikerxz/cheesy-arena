// Copyright 2018 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Model representing and methods for controlling a remote web display.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	minDisplayId       = 100
	displayPurgeTtlMin = 30
)

type DisplayType int

const (
	InvalidDisplay DisplayType = iota
	PlaceholderDisplay
	AllianceStationDisplay
	AnnouncerDisplay
	AudienceDisplay
	BracketDisplay
	FieldMonitorDisplay
	LogoDisplay
	QueueingDisplay
	RankingsDisplay
	TwitchStreamDisplay
	WallDisplay
	WebpageDisplay
)

var DisplayTypeNames = map[DisplayType]string{
	PlaceholderDisplay:     "Placeholder",
	AllianceStationDisplay: "Alliance Station",
	AnnouncerDisplay:       "Announcer",
	AudienceDisplay:        "Audience",
	BracketDisplay:         "Bracket",
	FieldMonitorDisplay:    "Field Monitor",
	LogoDisplay:            "Logo",
	QueueingDisplay:        "Queueing",
	RankingsDisplay:        "Rankings",
	TwitchStreamDisplay:    "Twitch Stream",
	WallDisplay:            "Wall",
	WebpageDisplay:         "Web Page",
}

var displayTypePaths = map[DisplayType]string{
	PlaceholderDisplay:     "/display",
	AllianceStationDisplay: "/displays/alliance_station",
	AnnouncerDisplay:       "/displays/announcer",
	AudienceDisplay:        "/displays/audience",
	BracketDisplay:         "/displays/bracket",
	FieldMonitorDisplay:    "/displays/field_monitor",
	LogoDisplay:            "/displays/logo",
	QueueingDisplay:        "/displays/queueing",
	RankingsDisplay:        "/displays/rankings",
	TwitchStreamDisplay:    "/displays/twitch",
	WallDisplay:            "/displays/wall",
	WebpageDisplay:         "/displays/webpage",
}

var displayRegistryMutex sync.Mutex

type Display struct {
	DisplayConfiguration DisplayConfiguration
	IpAddress            string
	ConnectionCount      int
	Notifier             *websocket.Notifier
	lastConnectedTime    time.Time
}

type DisplayConfiguration struct {
	Id            string
	Nickname      string
	Type          DisplayType
	Configuration map[string]string
	Persistent    bool
}

// Parses the given display URL path and query string to extract the configuration.
func DisplayFromUrl(path string, query map[string][]string) (*DisplayConfiguration, error) {
	if _, ok := query["displayId"]; !ok {
		return nil, fmt.Errorf("Display ID not present in request.")
	}

	var displayConfig DisplayConfiguration
	displayConfig.Id = query["displayId"][0]
	if nickname, ok := query["nickname"]; ok {
		displayConfig.Nickname, _ = url.QueryUnescape(nickname[0])
	}

	// Determine type from the websocket connection URL. This way of doing it isn't super efficient, but it's not really
	// a concern since it should happen relatively infrequently.
	for displayType, displayPath := range displayTypePaths {
		if path == displayPath+"/websocket" {
			displayConfig.Type = displayType
			break
		}
	}
	if displayConfig.Type == InvalidDisplay {
		return nil, fmt.Errorf("Could not determine display type from path %s.", path)
	}

	// Put any remaining query parameters into the per-type configuration map.
	displayConfig.Configuration = make(map[string]string)
	for key, value := range query {
		if key != "displayId" && key != "nickname" {
			displayConfig.Configuration[key], _ = url.QueryUnescape(value[0])
		}
	}

	return &displayConfig, nil
}

// Returns the URL string for the given display that includes all of its configuration parameters.
func (display *Display) ToUrl() string {
	var builder strings.Builder
	builder.WriteString(displayTypePaths[display.DisplayConfiguration.Type])
	builder.WriteString("?displayId=")
	builder.WriteString(url.QueryEscape(display.DisplayConfiguration.Id))
	if display.DisplayConfiguration.Nickname != "" {
		builder.WriteString("&nickname=")
		builder.WriteString(url.QueryEscape(display.DisplayConfiguration.Nickname))
	}

	// Sort the keys so that the URL generated is deterministic.
	var keys []string
	for key := range display.DisplayConfiguration.Configuration {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString("&")
		builder.WriteString(url.QueryEscape(key))
		builder.WriteString("=")
		builder.WriteString(url.QueryEscape(display.DisplayConfiguration.Configuration[key]))
	}
	return builder.String()
}

func (display *Display) generateDisplayConfigurationMessage() any {
	return display.ToUrl()
}

// Returns an unused ID that can be used for a new display.
func (arena *Arena) NextDisplayId() string {
	displayRegistryMutex.Lock()
	defer displayRegistryMutex.Unlock()

	// Loop until we get an ID that isn't already used. This is inefficient if there is a large number of displays, but
	// that should never be the case.
	candidateId := minDisplayId
	for {
		if _, ok := arena.Displays[strconv.Itoa(candidateId)]; !ok {
			return strconv.Itoa(candidateId)
		}
		candidateId++
	}
}

// Creates or gets the given display in the arena registry and triggers a notification.
func (arena *Arena) RegisterDisplay(displayConfig *DisplayConfiguration, ipAddress string) *Display {
	displayRegistryMutex.Lock()
	defer displayRegistryMutex.Unlock()

	display, ok := arena.Displays[displayConfig.Id]
	if !ok {
		if savedConfig, err := arena.Database.GetDisplayConfiguration(displayConfig.Id); err == nil && savedConfig != nil {
			display = newDisplayFromModelConfig(*savedConfig)
			arena.Displays[displayConfig.Id] = display
			ok = true
		}
	}
	if ok {
		display.ConnectionCount++
		display.IpAddress = ipAddress
		display.lastConnectedTime = time.Now()
		// If this display is marked persistent, ignore any incoming configuration changes from the client.
		if !display.DisplayConfiguration.Persistent && displayConfig.Type != PlaceholderDisplay {
			display.DisplayConfiguration = *displayConfig
			display.Notifier.Notify()
		} else {
			display.Notifier.Notify()
		}
	} else {
		display = new(Display)
		display.Notifier = websocket.NewNotifier(
			"displayConfiguration", display.generateDisplayConfigurationMessage,
		)
		display.DisplayConfiguration = *displayConfig
		display.IpAddress = ipAddress
		display.ConnectionCount += 1
		display.lastConnectedTime = time.Now()
		arena.Displays[displayConfig.Id] = display
		display.Notifier.Notify()
	}
	arena.DisplayConfigurationNotifier.Notify()

	return display
}

// Updates the given display in the arena registry. Triggers a notification if the display configuration changed.
func (arena *Arena) UpdateDisplay(displayConfig DisplayConfiguration) error {
	displayRegistryMutex.Lock()
	display, ok := arena.Displays[displayConfig.Id]
	if !ok {
		display = new(Display)
		display.Notifier = websocket.NewNotifier(
			"displayConfiguration", display.generateDisplayConfigurationMessage,
		)
		arena.Displays[displayConfig.Id] = display
	}
	changed := !reflect.DeepEqual(displayConfig, display.DisplayConfiguration)
	if changed {
		display.DisplayConfiguration = displayConfig
		display.Notifier.Notify()
		arena.DisplayConfigurationNotifier.Notify()
	}
	displayRegistryMutex.Unlock()

	if !changed {
		return nil
	}
	if displayConfig.Persistent {
		return arena.Database.SaveDisplayConfiguration(convertToModelDisplayConfig(displayConfig))
	}
	return arena.Database.DeleteDisplayConfiguration(displayConfig.Id)
}

// Removes a display's persistent configuration and marks it non-persistent in memory.
func (arena *Arena) RemoveDisplay(displayId string) error {
	displayRegistryMutex.Lock()
	if display, ok := arena.Displays[displayId]; ok {
		display.DisplayConfiguration.Persistent = false
	}
	displayRegistryMutex.Unlock()
	if err := arena.Database.DeleteDisplayConfiguration(displayId); err != nil {
		return err
	}
	arena.DisplayConfigurationNotifier.Notify()
	return nil
}

// Marks the given display as having disconnected in the arena registry and triggers a notification.
func (arena *Arena) MarkDisplayDisconnected(displayId string) {
	displayRegistryMutex.Lock()
	defer displayRegistryMutex.Unlock()

	if existingDisplay, ok := arena.Displays[displayId]; ok {
		if existingDisplay.ConnectionCount == 1 && existingDisplay.DisplayConfiguration.Type == PlaceholderDisplay &&
			existingDisplay.DisplayConfiguration.Nickname == "" &&
			len(existingDisplay.DisplayConfiguration.Configuration) == 0 {
			// If the display is an unconfigured placeholder, just remove it entirely to prevent clutter.
			delete(arena.Displays, existingDisplay.DisplayConfiguration.Id)
		} else {
			existingDisplay.ConnectionCount -= 1
		}
		existingDisplay.lastConnectedTime = time.Now()
		arena.DisplayConfigurationNotifier.Notify()
	}
}

// Removes any displays from the list that haven't had any active connections for a while and don't have a nickname.
func (arena *Arena) purgeDisconnectedDisplays() {
	displayRegistryMutex.Lock()
	defer displayRegistryMutex.Unlock()

	deleted := false
	for id, display := range arena.Displays {
		if display.ConnectionCount == 0 && display.DisplayConfiguration.Nickname == "" &&
			time.Now().Sub(display.lastConnectedTime).Minutes() >= displayPurgeTtlMin {
			delete(arena.Displays, id)
			deleted = true
		}
	}
	if deleted {
		arena.DisplayConfigurationNotifier.Notify()
	}
}

func convertToModelDisplayConfig(displayConfig DisplayConfiguration) *model.DisplayConfiguration {
	configCopy := make(map[string]string)
	for key, value := range displayConfig.Configuration {
		configCopy[key] = value
	}
	return &model.DisplayConfiguration{
		DisplayId:     displayConfig.Id,
		Nickname:      displayConfig.Nickname,
		Type:          int(displayConfig.Type),
		Configuration: configCopy,
		Persistent:    displayConfig.Persistent,
	}
}

func newDisplayFromModelConfig(modelConfig model.DisplayConfiguration) *Display {
	display := new(Display)
	display.Notifier = websocket.NewNotifier("displayConfiguration", display.generateDisplayConfigurationMessage)
	display.DisplayConfiguration = DisplayConfiguration{
		Id:            modelConfig.DisplayId,
		Nickname:      modelConfig.Nickname,
		Type:          DisplayType(modelConfig.Type),
		Configuration: make(map[string]string),
		Persistent:    modelConfig.Persistent,
	}
	for key, value := range modelConfig.Configuration {
		display.DisplayConfiguration.Configuration[key] = value
	}
	display.lastConnectedTime = time.Now()
	return display
}
