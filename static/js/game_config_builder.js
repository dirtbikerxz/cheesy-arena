let builder;

class GameConfigBuilder {
  constructor() {
    this.currentPanel = "red_near";
    this.panels = {};
    this.scoring = [];
    this.selectedWidgetId = null;
    this.loadInitialConfig();
    this.bindPalette();
    this.bindTabs();
    this.bindProperties();
    this.renderPanel();
    this.renderScoringList();
  }

  loadInitialConfig() {
    const raw = document.getElementById("initialConfig").textContent || "{}";
    try {
      this.config = JSON.parse(raw);
    } catch {
      this.config = { name: "Custom Game", version: "1.0.0", panels: [], scoring: [] };
    }
    document.getElementById("gameName").value = this.config.name || "Custom Game";
    this.scoring = this.config.scoring || [];
    (this.config.panels || []).forEach((panel) => (this.panels[panel.id] = panel));
    ["red_near", "red_far", "blue_near", "blue_far", "referee", "head_ref"].forEach((id) => {
      if (!this.panels[id]) this.panels[id] = { id, title: id.replace("_", " "), widgets: [] };
    });
  }

  bindPalette() {
    document.querySelectorAll(".palette-item").forEach((item) => {
      item.addEventListener("dragstart", (e) => {
        e.dataTransfer.setData("type", e.target.dataset.type);
        e.dataTransfer.effectAllowed = "copy";
      });
      item.addEventListener("click", () => this.addWidget(item.dataset.type));
    });
  }

  bindTabs() {
    document.querySelectorAll(".panel-tab").forEach((btn) => {
      btn.addEventListener("click", (e) => {
        document.querySelectorAll(".panel-tab").forEach((tab) => tab.classList.remove("active"));
        e.target.classList.add("active");
        this.currentPanel = e.target.dataset.panel;
        this.renderPanel();
      });
    });
  }

  bindCanvasEvents(canvas) {
    canvas.addEventListener("dragover", (e) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = "copy";
      canvas.classList.remove("empty");
    });
    canvas.addEventListener("dragenter", (e) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = "copy";
      canvas.classList.remove("empty");
    });
    canvas.addEventListener("drop", (e) => {
      e.preventDefault();
      const type = e.dataTransfer.getData("type") || e.dataTransfer.getData("text/plain");
      if (type) this.addWidget(type);
    });
  }

  widgetTemplate(widget) {
    const el = document.createElement("div");
    el.className = "widget";
    el.dataset.id = widget.id;
    el.dataset.type = widget.type;
    el.style.borderColor = widget.color || "";
    el.style.gridColumn = `span ${widget.position?.colSpan || 1}`;
    el.innerHTML = `
      <div class="title">${widget.label || widget.type}</div>
      <div class="meta">
        <span class="badge-type">${widget.type}</span>
        <span>${widget.points || ""}</span>
      </div>
    `;
    el.addEventListener("click", () => this.selectWidget(widget.id));
    return el;
  }

  renderPanel() {
    const canvas = document.getElementById("panel-canvas");
    canvas.innerHTML = "";
    canvas.dataset.panel = this.currentPanel;
    canvas.classList.add("active");
    this.bindCanvasEvents(canvas);
    const widgets = this.panels[this.currentPanel].widgets || [];
    if (widgets.length === 0) canvas.classList.add("empty");
    else canvas.classList.remove("empty");

    widgets.forEach((widget) => {
      const el = this.widgetTemplate(widget);
      canvas.appendChild(el);
    });

    if (widgets.length) this.selectWidget(widgets[0].id);
    else this.syncProperties(null);
  }

  addWidget(type) {
    const panel = this.panels[this.currentPanel];
    if (!panel.widgets) panel.widgets = [];
    const id = `${type}_${Date.now()}`;
    const widget = {
      id,
      type,
      label: type.charAt(0).toUpperCase() + type.slice(1),
      points: type === "foul" ? "-3" : "+1",
      phase: "any",
      color: "#22d3ee",
      position: { row: 1, col: 1, colSpan: 1 },
    };
    panel.widgets.push(widget);
    this.renderPanel();
    this.selectWidget(id);
  }

  selectWidget(id) {
    this.selectedWidgetId = id;
    document.querySelectorAll(".widget").forEach((w) => w.classList.toggle("selected", w.dataset.id === id));
    this.syncProperties(this.getSelectedWidget());
  }

  getSelectedWidget() {
    const panel = this.panels[this.currentPanel];
    return (panel.widgets || []).find((w) => w.id === this.selectedWidgetId);
  }

  bindProperties() {
    document.getElementById("propLabel").addEventListener("input", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.label = e.target.value;
      this.renderPanel();
      this.selectWidget(widget.id);
    });
    document.getElementById("propId").addEventListener("input", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.id = e.target.value;
      this.renderPanel();
      this.selectWidget(widget.id);
    });
    document.getElementById("propPoints").addEventListener("input", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.points = e.target.value;
    });
    document.getElementById("propScoring").addEventListener("change", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.scoringId = e.target.value;
    });
    document.getElementById("propPhase").addEventListener("change", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.phase = e.target.value;
    });
    document.getElementById("propRow").addEventListener("input", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.position = widget.position || {};
      widget.position.row = parseInt(e.target.value || "1", 10);
    });
    document.getElementById("propCol").addEventListener("input", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.position = widget.position || {};
      widget.position.col = parseInt(e.target.value || "1", 10);
    });
    document.getElementById("propColSpan").addEventListener("input", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.position = widget.position || {};
      widget.position.colSpan = parseInt(e.target.value || "1", 10);
      this.renderPanel();
      this.selectWidget(widget.id);
    });
    document.getElementById("propColor").addEventListener("input", (e) => {
      const widget = this.getSelectedWidget();
      if (!widget) return;
      widget.color = e.target.value;
      this.renderPanel();
      this.selectWidget(widget.id);
    });
  }

  syncProperties(widget) {
    const emptyState = document.getElementById("propertiesEmpty");
    const formState = document.getElementById("propertiesForm");
    if (!widget) {
      emptyState.classList.remove("d-none");
      formState.classList.add("d-none");
      return;
    }
    emptyState.classList.add("d-none");
    formState.classList.remove("d-none");
    document.getElementById("propLabel").value = widget.label || "";
    document.getElementById("propId").value = widget.id || "";
    document.getElementById("propPoints").value = widget.points || "";
    document.getElementById("propPhase").value = widget.phase || "any";
    document.getElementById("propColor").value = widget.color || "#22d3ee";
    document.getElementById("propRow").value = widget.position?.row || 1;
    document.getElementById("propCol").value = widget.position?.col || 1;
    document.getElementById("propColSpan").value = widget.position?.colSpan || 1;
    this.populateScoringSelect(widget.scoringId);
  }

  populateScoringSelect(selectedId) {
    const select = document.getElementById("propScoring");
    select.innerHTML = '<option value="">None</option>';
    this.scoring.forEach((s) => {
      const opt = document.createElement("option");
      opt.value = s.id;
      opt.textContent = `${s.label} (${s.pointValue} pts)`;
      if (s.id === selectedId) opt.selected = true;
      select.appendChild(opt);
    });
  }

  deleteSelected() {
    const panel = this.panels[this.currentPanel];
    if (!this.selectedWidgetId || !panel.widgets) return;
    panel.widgets = panel.widgets.filter((w) => w.id !== this.selectedWidgetId);
    this.selectedWidgetId = null;
    this.renderPanel();
  }

  addScoringElement() {
    const id = `score_${Date.now()}`;
    this.scoring.push({ id, label: "Scoring Item", pointValue: 1, phase: "any" });
    this.renderScoringList();
    this.populateScoringSelect(this.getSelectedWidget()?.scoringId);
  }

  renderScoringList() {
    const tbody = document.querySelector("#scoringList tbody");
    if (!tbody) return;
    tbody.innerHTML = "";
    this.scoring.forEach((s, idx) => {
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td><input class="form-control form-control-sm bg-body" value="${s.label}" data-field="label" data-idx="${idx}"></td>
        <td><input class="form-control form-control-sm bg-body" value="${s.id}" data-field="id" data-idx="${idx}"></td>
        <td><input type="number" class="form-control form-control-sm bg-body" value="${s.pointValue}" data-field="pointValue" data-idx="${idx}"></td>
        <td>
          <select class="form-select form-select-sm bg-body" data-field="phase" data-idx="${idx}">
            <option value="any"${s.phase === "any" ? " selected" : ""}>Any</option>
            <option value="auto"${s.phase === "auto" ? " selected" : ""}>Auto</option>
            <option value="teleop"${s.phase === "teleop" ? " selected" : ""}>Teleop</option>
            <option value="endgame"${s.phase === "endgame" ? " selected" : ""}>Endgame</option>
          </select>
        </td>
        <td><button class="btn btn-sm btn-outline-danger" data-remove="${idx}">Delete</button></td>
      `;
      tbody.appendChild(tr);
    });
    tbody.querySelectorAll("input,select").forEach((el) => {
      el.addEventListener("input", (e) => {
        const idx = parseInt(e.target.dataset.idx, 10);
        const field = e.target.dataset.field;
        if (!this.scoring[idx]) return;
        if (field === "pointValue") this.scoring[idx][field] = parseInt(e.target.value || "0", 10);
        else this.scoring[idx][field] = e.target.value;
        this.populateScoringSelect(this.getSelectedWidget()?.scoringId);
      });
    });
    tbody.querySelectorAll("button[data-remove]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const idx = parseInt(btn.dataset.remove, 10);
        this.scoring.splice(idx, 1);
        this.renderScoringList();
        this.populateScoringSelect(this.getSelectedWidget()?.scoringId);
      });
    });
  }

  exportConfig() {
    const data = this.collectConfig();
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${data.name || "game-config"}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  importFile(event) {
    const file = event.target.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      try {
        const parsed = JSON.parse(e.target.result);
        this.config = parsed;
        this.panels = {};
        this.scoring = parsed.scoring || [];
        (parsed.panels || []).forEach((panel) => (this.panels[panel.id] = panel));
        this.renderPanel();
        this.renderScoringList();
      } catch {
        alert("Invalid config file");
      }
    };
    reader.readAsText(file);
  }

  collectConfig() {
    const panels = Object.values(this.panels).map((panel) => ({
      ...panel,
      widgets: panel.widgets || [],
    }));
    return {
      ...(this.config || {}),
      name: document.getElementById("gameName").value || "Custom Game",
      panels,
      scoring: this.scoring,
    };
  }

  saveConfig() {
    const data = this.collectConfig();
    document.getElementById("config").value = JSON.stringify(data, null, 2);
    document.getElementById("configForm").submit();
  }
}

window.addEventListener("DOMContentLoaded", () => {
  builder = new GameConfigBuilder();
});
