// Copyright 2018 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side logic for the display configuration page.

var displayTemplate = Handlebars.compile($("#displayTemplate").html());
var websocket;
var fieldsChanged = false;

var configureDisplay = function (displayId) {
  // Convert configuration string into map.
  var configurationMap = {};
  var rawConfig = $("#displayConfiguration" + displayId).val();
  if (rawConfig.trim().length > 0) {
    $.each(rawConfig.split("&"), function (index, param) {
      if (!param) {
        return;
      }
      var keyValuePair = param.split("=");
      configurationMap[keyValuePair[0]] = keyValuePair[1];
    });
  }

  fieldsChanged = false;
  websocket.send("configureDisplay", {
    Id: displayId,
    Nickname: $("#displayNickname" + displayId).val(),
    Type: parseInt($("#displayType" + displayId).val()),
    Configuration: configurationMap,
    Persistent: $("#displayPersistent" + displayId).is(":checked")
  });
};

var undoChanges = function () {
  window.location.reload();
};

var reloadDisplay = function (displayId) {
  websocket.send("reloadDisplay", displayId);
};

var reloadAllDisplays = function () {
  websocket.send("reloadAllDisplays");
};

var removeDisplay = function (displayId) {
  if (!confirm("Remove persistent configuration for display " + displayId + "?")) {
    return;
  }
  websocket.send("removeDisplay", displayId);
};

var addDisplay = function () {
  var newId = $("#newDisplayId").val();
  if (!newId) {
    alert("Please provide a display ID.");
    return;
  }
  var configurationMap = {};
  var rawConfig = $("#newDisplayConfiguration").val();
  if (rawConfig.trim().length > 0) {
    $.each(rawConfig.split("&"), function (index, param) {
      if (!param) {
        return;
      }
      var keyValuePair = param.split("=");
      configurationMap[keyValuePair[0]] = keyValuePair[1];
    });
  }
  websocket.send("configureDisplay", {
    Id: newId,
    Nickname: $("#newDisplayNickname").val(),
    Type: parseInt($("#newDisplayType").val()),
    Configuration: configurationMap,
    Persistent: $("#newDisplayPersistent").is(":checked")
  });
  $("#newDisplayId").val("");
  $("#newDisplayNickname").val("");
  $("#newDisplayConfiguration").val("");
  $("#newDisplayPersistent").prop("checked", true);
};

// Register that an input element has been modified by the user to avoid overwriting with a server update.
var markChanged = function (element) {
  fieldsChanged = true;
  element.setAttribute("data-changed", true);
};

// Handles a websocket message to refresh the display list.
var handleDisplayConfiguration = function (data) {
  if (fieldsChanged) {
    // Don't overwrite anything if the user has made unsaved changes.
    return;
  }

  $("#persistentDisplayContainer").empty();
  $("#transientDisplayContainer").empty();

  $.each(data, function (displayId, display) {
    var displayRow = displayTemplate(display);
    if (display.DisplayConfiguration.Persistent) {
      $("#persistentDisplayContainer").append(displayRow);
    } else {
      $("#transientDisplayContainer").append(displayRow);
    }
    $("#displayNickname" + displayId).val(display.DisplayConfiguration.Nickname);
    $("#displayType" + displayId).val(display.DisplayConfiguration.Type);
    $("#displayPersistent" + displayId).prop("checked", display.DisplayConfiguration.Persistent);

    // Convert configuration map to query string format.
    var configurationString = $.map(Object.entries(display.DisplayConfiguration.Configuration), function (entry) {
      return entry.join("=");
    }).join("&");
    $("#displayConfiguration" + displayId).val(configurationString);
  });
};

$(function () {
  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/setup/displays/websocket", {
    displayConfiguration: function (event) {
      handleDisplayConfiguration(event.data);
    }
  });
});
