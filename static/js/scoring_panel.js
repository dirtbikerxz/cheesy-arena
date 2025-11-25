// Dynamic scoring panel powered by the game configuration.

let websocket;
let alliance;
let scoringAvailable = false;
let commitAvailable = false;
let committed = false;
let currentPhase = "pregame";
let redFoulDigest = "";
let blueFoulDigest = "";

const state = {
  widgets: {},
};

const parseWidget = (el) => {
  return {
    id: el.dataset.widgetId,
    type: el.dataset.widgetType,
    points: parseInt(el.dataset.points || "0", 10),
    states: el.dataset.states || "",
    el,
  };
};

const initWidgets = () => {
  document.querySelectorAll(".widget-card").forEach((el) => {
    const widget = parseWidget(el);
    state.widgets[widget.id] = widget;

    if (widget.type === "counter") {
      el.querySelector(".widget-inc")?.addEventListener("click", () => sendWidget(widget.id, { delta: 1 }));
      el.querySelector(".widget-dec")?.addEventListener("click", () => sendWidget(widget.id, { delta: -1 }));
    } else if (widget.type === "toggle") {
      el.querySelector(".widget-toggle")?.addEventListener("click", (e) => {
        e.preventDefault();
        sendWidget(widget.id, { action: "toggle" });
      });
    } else if (widget.type === "multistate") {
      el.querySelectorAll(".widget-state").forEach((btn) => {
        btn.addEventListener("click", (e) => {
          e.preventDefault();
          e.stopPropagation();
          const alreadyActive = btn.classList.contains("active");
          el.querySelectorAll(".widget-state").forEach((b) => b.classList.remove("active"));
          if (alreadyActive) {
            sendWidget(widget.id, { state: "" });
          } else {
            btn.classList.add("active");
            sendWidget(widget.id, { state: btn.dataset.state });
          }
        });
      });
    } else if (widget.type === "foul") {
      el.querySelector(".widget-foul")?.addEventListener("click", () => {
        addFoul(alliance === "blue" ? "red" : "blue", false);
      });
    }
  });
};

const connect = () => {
  const pathParts = window.location.pathname.split("/");
  const position = pathParts[pathParts.length - 1];
  alliance = position.split("_")[0];
  document.body.dataset.alliance = alliance;

  websocket = new CheesyWebsocket("/panels/scoring/" + position + "/websocket", {
    matchLoad: (event) => handleMatchLoad(event.data),
    matchTime: (event) => handleMatchTime(event.data),
    realtimeScore: (event) => handleRealtimeScore(event.data),
    resetLocalState: () => resetLocalState(),
  });
};

const sendWidget = (widgetId, opts) => {
  websocket.send("widget", {
    WidgetId: widgetId,
    Delta: opts.delta || 0,
    Action: opts.action || "",
    State: opts.state || "",
  });
};

const handleMatchLoad = (data) => {
  $("#matchName").text(data.Match.LongName);
  committed = false;
};

const handleMatchTime = (data) => {
  switch (matchStates[data.MatchState]) {
    case "AUTO_PERIOD":
      currentPhase = "auto";
      scoringAvailable = true;
      commitAvailable = false;
      break;
    case "PAUSE_PERIOD":
      currentPhase = "auto";
      scoringAvailable = true;
      commitAvailable = false;
      break;
    case "TELEOP_PERIOD":
      currentPhase = "teleop";
      scoringAvailable = true;
      commitAvailable = false;
      break;
    case "POST_MATCH":
      currentPhase = "post";
      scoringAvailable = true;
      commitAvailable = !committed;
      break;
    default:
      currentPhase = "pregame";
      scoringAvailable = false;
      commitAvailable = false;
  }
  updateUiState();
};

const handleRealtimeScore = (data) => {
  const realtimeScore = alliance === "red" ? data.Red : data.Blue;
  const score = realtimeScore.Score;

  // Counters
  Object.entries(score.GenericCounters || {}).forEach(([id, val]) => {
    document.querySelector(`[data-widget-id="${id}"] .widget-value`)?.replaceChildren(document.createTextNode(val));
  });
  // Toggles
  Object.entries(score.GenericToggles || {}).forEach(([id, val]) => {
    const btn = document.querySelector(`[data-widget-id="${id}"] .widget-toggle`);
    if (btn) {
      btn.classList.toggle("active", !!val);
    }
  });
  // Multistate
  Object.entries(score.GenericStates || {}).forEach(([id, state]) => {
    document.querySelectorAll(`[data-widget-id="${id}"] .widget-state`).forEach((btn) => {
      const isActive = btn.dataset.state === state;
      btn.classList.toggle("active", isActive);
      btn.setAttribute("aria-pressed", isActive);
    });
  });

  // Update foul list if changed.
  const newRedDigest = JSON.stringify(data.Red.Score.Fouls || []);
  const newBlueDigest = JSON.stringify(data.Blue.Score.Fouls || []);
  if (newRedDigest !== redFoulDigest || newBlueDigest !== blueFoulDigest) {
    redFoulDigest = newRedDigest;
    blueFoulDigest = newBlueDigest;
    renderFoulList(data);
  }
};

const commitMatchScore = () => {
  websocket.send("commitMatch", {});
  committed = true;
  commitAvailable = false;
  updateUiState();
};

const addFoul = (foulAlliance, isMajor) => {
  websocket.send("addFoul", { Alliance: foulAlliance, IsMajor: isMajor });
};

const toggleFoulType = (foulAlliance, index) => {
  websocket.send("toggleFoulType", { Alliance: foulAlliance, Index: index });
};
const updateFoulTeam = (foulAlliance, index, teamId) => {
  websocket.send("updateFoulTeam", { Alliance: foulAlliance, Index: index, TeamId: teamId });
};
const updateFoulRule = (foulAlliance, index, ruleId) => {
  websocket.send("updateFoulRule", { Alliance: foulAlliance, Index: index, RuleId: ruleId });
};
const deleteFoul = (foulAlliance, index) => {
  websocket.send("deleteFoul", { Alliance: foulAlliance, Index: index });
};

const renderFoulList = (data) => {
  const container = document.getElementById("foulList");
  if (!container) return;
  const buildAlliance = (alliance, fouls) => {
    return `
      <div class="foul-list-alliance">
        <div class="foul-list-header ${alliance}">${alliance.toUpperCase()} Fouls (${fouls.length})</div>
        <div class="foul-list-items">
          ${fouls
            .map(
              (foul, idx) => `
              <div class="foul-row">
                <div class="foul-index">${idx + 1}</div>
                <button class="foul-type ${foul.IsMajor ? "major" : "minor"}" onclick="toggleFoulType('${alliance}', ${idx});">
                  ${foul.IsMajor ? "Major" : "Minor"}
                </button>
                <button class="foul-delete" onclick="deleteFoul('${alliance}', ${idx});">Remove</button>
              </div>
            `,
            )
            .join("")}
          ${fouls.length === 0 ? '<div class="foul-empty text-muted">No fouls yet.</div>' : ""}
        </div>
      </div>
    `;
  };
  container.innerHTML = buildAlliance("blue", data.Blue.Score.Fouls || []) + buildAlliance("red", data.Red.Score.Fouls || []);
};

const resetLocalState = () => {};

const updateUiState = () => {
  document.querySelectorAll(".widget-card").forEach((card) => {
    const phase = card.dataset.phase || "any";
    const disableForPhase =
      phase === "auto"
        ? currentPhase !== "auto"
        : phase === "teleop"
        ? currentPhase !== "teleop"
        : phase === "endgame"
        ? !(currentPhase === "post" || currentPhase === "teleop")
        : false;
    card.querySelectorAll("button").forEach((btn) => {
      btn.disabled = !scoringAvailable || disableForPhase;
    });
  });
  $("#commit").prop("disabled", !commitAvailable);
  $("#fouls-button").prop("disabled", !scoringAvailable);
};

window.addEventListener("load", () => {
  initWidgets();
  connect();
});

window.addFoul = addFoul;
window.commitMatchScore = commitMatchScore;
window.toggleFoulType = toggleFoulType;
window.updateFoulTeam = updateFoulTeam;
window.updateFoulRule = updateFoulRule;
window.deleteFoul = deleteFoul;
window.openFoulDialog = () => {
  document.getElementById("fouls-dialog").showModal();
};
window.closeFoulsDialog = () => {
  document.getElementById("fouls-dialog").close();
};
window.closeFoulsDialogIfOutside = (event) => {
  const dialog = document.getElementById("fouls-dialog");
  if (event.target === dialog) {
    closeFoulsDialog();
  }
};
