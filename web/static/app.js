/* ==========================================================================
   AWM Kanban — Application JS
   Vanilla JS, no build step. Uses fetch for API calls.
   ========================================================================== */

(function () {
  "use strict";

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  // Board columns — "done" absorbs complete + superseded.
  const BOARD_COLUMNS = ["pending", "in_progress", "blocked", "done"];

  const COLUMN_LABELS = {
    pending: "Pending",
    in_progress: "In Progress",
    blocked: "Blocked",
    done: "Done",
  };

  // All known task statuses and their display labels (used elsewhere).
  const STATUSES = ["pending", "in_progress", "complete", "blocked", "superseded"];

  const STATUS_LABELS = {
    pending: "Pending",
    in_progress: "In Progress",
    complete: "Complete",
    blocked: "Blocked",
    superseded: "Superseded",
    done: "Done",
  };

  // Map raw task status to a board column.
  function boardColumn(status) {
    if (status === "complete" || status === "superseded") return "done";
    if (BOARD_COLUMNS.includes(status)) return status;
    return "pending";
  }

  function esc(str) {
    const el = document.createElement("span");
    el.textContent = str;
    return el.innerHTML;
  }

  function shortKey(key) {
    if (!key) return "";
    // e.g. "impl:receipt-handler" -> "impl:receipt-handler"
    // keep it readable but cap length
    return key.length > 48 ? key.slice(0, 45) + "\u2026" : key;
  }

  // ---------------------------------------------------------------------------
  // API
  // ---------------------------------------------------------------------------

  async function fetchJSON(url) {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  }

  async function getPlans(scope) {
    const url = scope ? "/api/plans?scope=" + encodeURIComponent(scope) : "/api/plans";
    const data = await fetchJSON(url);
    return data.items || [];
  }

  async function getPlanDetail(key) {
    const data = await fetchJSON("/api/plans/" + encodeURIComponent(key));
    return data.document || {};
  }

  async function getStatus() {
    return fetchJSON("/api/status");
  }

  
  // ---------------------------------------------------------------------------
  // Board Page
  // ---------------------------------------------------------------------------

  function isboardPage() {
    const p = location.pathname;
    return p === "/" || p === "/index.html";
  }

  // ---------------------------------------------------------------------------
  // Task Type System — map key prefixes to human-friendly types
  // ---------------------------------------------------------------------------

  const TASK_TYPES = {
    stage:  { label: "Stage",        cls: "type-stage",  scaffolding: true },
    spec:   { label: "Spec",         cls: "type-spec",   scaffolding: true },
    refine: { label: "Refinement",   cls: "type-refine", scaffolding: true },
    impl:   { label: "Task",         cls: "type-impl",   scaffolding: false },
    verify: { label: "Verification", cls: "type-verify", scaffolding: false },
    review: { label: "Review",       cls: "type-review", scaffolding: false },
    tdd:    { label: "TDD",          cls: "type-tdd",    scaffolding: false },
  };

  function taskType(key) {
    if (!key) return null;
    const prefix = key.split(":")[0];
    return TASK_TYPES[prefix] || null;
  }

  function isScaffolding(key) {
    const type = taskType(key);
    return type ? type.scaffolding : false;
  }

  // Store all tasks globally so modal/navigation can find anything.
  let _boardTasks = [];
  let _tasksByKey = {};
  let _childrenOf = {}; // parent_key -> [idx, ...]

  function buildIndex(allTasks) {
    _boardTasks = allTasks;
    _tasksByKey = {};
    _childrenOf = {};
    for (let i = 0; i < allTasks.length; i++) {
      const t = allTasks[i];
      if (t.key) {
        _tasksByKey[t.key] = i;
      }
      if (t.parent_task_key) {
        if (!_childrenOf[t.parent_task_key]) _childrenOf[t.parent_task_key] = [];
        _childrenOf[t.parent_task_key].push(i);
      }
    }
  }

  function renderCard(task, idx) {
    const statusCls = STATUSES.includes(task.status) ? task.status : "pending";
    const type = taskType(task.key);

    let tagsHtml = "";
    if (type) {
      tagsHtml += `<span class="card-type ${type.cls}">${esc(type.label)}</span>`;
    }

    // Show parent context as a subtle label (for flat view)
    let parentLabel = "";
    if (task.parent_task_key) {
      const parentIdx = _tasksByKey[task.parent_task_key];
      if (parentIdx !== undefined) {
        const parent = _boardTasks[parentIdx];
        parentLabel = parent.summary || task.parent_task_key;
      }
    }

    return `
      <div class="task-card ${statusCls}" onclick="AWM.openCard(${idx})">
        ${tagsHtml ? `<div class="card-tags">${tagsHtml}</div>` : ""}
        <div class="card-summary">${esc(task.summary || "")}</div>
        ${parentLabel ? `<div class="card-footer"><span class="card-parent-ctx">${esc(parentLabel)}</span></div>` : ""}
      </div>`;
  }

  // Compute status counts from a task list.
  function taskCounts(tasks) {
    const counts = { total: tasks.length, pending: 0, in_progress: 0, complete: 0, blocked: 0, superseded: 0 };
    for (const t of tasks) {
      if (counts[t.status] !== undefined) counts[t.status]++;
      else counts.pending++;
    }
    counts.done = counts.complete + counts.superseded;
    return counts;
  }

  // Render progress bar for a set of counts.
  function renderProgress(counts) {
    const pct = counts.total ? Math.round((counts.done / counts.total) * 100) : 0;
    return `
      <div class="plan-progress">
        <div class="plan-progress-bar">
          <div class="plan-progress-fill" style="width:${pct}%"></div>
        </div>
        <span class="plan-progress-text">${counts.done}/${counts.total}</span>
      </div>`;
  }

  // Current board state — persists across polls.
  let _boardScope = "current";
  let _taskFilter = "work"; // "work" = hide scaffolding, "all" = show everything
  let _expandedPlans = new Set(); // track which plans are expanded by key or title

  function cleanPlanTitle(plan, detail) {
    if (detail && detail.plan && detail.plan.title) return detail.plan.title;
    const key = plan.plan_key || plan.key || "";
    if (key.startsWith("plan:receipt-")) {
      return key.replace("plan:receipt-", "receipt ").slice(0, 24) + "\u2026";
    }
    if (key.startsWith("plan:")) return key.slice(5);
    return key || "Untitled Plan";
  }

  // Determine overall plan status from a plan summary or detail.
  function planStatus(plan, detail) {
    if (plan.status) return plan.status;
    if (detail && detail.plan && detail.plan.status) return detail.plan.status;
    return "in_progress";
  }

  async function loadBoard() {
    const container = document.getElementById("board-container");
    if (!container) return;

    try {
      const plans = await getPlans(_boardScope);

      if (!plans.length) {
        container.innerHTML = `
          <div class="board-empty">
            <h2>No plans found</h2>
            <p>Create a plan with AWM to see tasks here.</p>
          </div>`;
        return;
      }

      // Fetch details for each plan in parallel
      const details = await Promise.allSettled(
        plans.map(async (plan) => {
          if (!plan.fetch_keys || !plan.fetch_keys.length) {
            return { plan, detail: null, tasks: [] };
          }
          const detail = await getPlanDetail(plan.fetch_keys[0]);
          const tasks =
            detail.plan && detail.plan.tasks ? detail.plan.tasks : [];
          return { plan, detail, tasks };
        })
      );

      // Build per-plan data and a global task list for the index.
      const allTasks = [];
      const planDataList = [];

      for (const result of details) {
        if (result.status !== "fulfilled") continue;
        const { plan, detail, tasks } = result.value;
        if (!tasks.length) continue;

        const title = cleanPlanTitle(plan, detail);
        const status = planStatus(plan, detail);
        const planKey = plan.plan_key || plan.key || "";
        const parentPlanKey = (detail && detail.plan && detail.plan.parent_plan_key)
          ? detail.plan.parent_plan_key
          : (plan.parent_plan_key || "");

        // All tasks go into the global index (for modal navigation).
        for (const task of tasks) {
          allTasks.push({ ...task, _planTitle: title });
        }

        // Filter tasks for display based on current filter.
        const visibleTasks = _taskFilter === "work"
          ? tasks.filter((t) => !isScaffolding(t.key))
          : tasks;

        planDataList.push({
          title,
          status,
          planKey,
          parentPlanKey,
          tasks: visibleTasks,
          allTasksForCounts: tasks, // always count from full set
          children: [], // filled below
        });
      }

      // Build index from ALL tasks (not filtered) so modal links work.
      buildIndex(allTasks);

      if (!planDataList.length) {
        container.innerHTML = `
          <div class="board-empty">
            <h2>No tasks found</h2>
            <p>Plans exist but contain no tasks yet.</p>
          </div>`;
        return;
      }

      // Build plan tree: group child plans under their parents.
      const plansByKey = {};
      for (const pd of planDataList) {
        if (pd.planKey) plansByKey[pd.planKey] = pd;
      }

      const rootPlans = [];
      for (const pd of planDataList) {
        if (pd.parentPlanKey && plansByKey[pd.parentPlanKey]) {
          plansByKey[pd.parentPlanKey].children.push(pd);
        } else {
          rootPlans.push(pd);
        }
      }

      // Auto-expand if there's only one root plan.
      if (rootPlans.length === 1 && _expandedPlans.size === 0) {
        _expandedPlans.add(rootPlans[0].planKey || rootPlans[0].title);
      }

      // Render plan tree recursively.
      let html = "";
      for (const pd of rootPlans) {
        html += renderPlanTree(pd, 0);
      }

      container.innerHTML = html;
    } catch (err) {
      console.error("Board load error:", err);
      container.innerHTML = `<div class="error-banner">Failed to load board: ${esc(err.message)}</div>`;
    }
  }

  // Collect all task keys from a plan's descendant plans (not the plan itself).
  function collectDescendantPlanTaskKeys(pd) {
    const keys = new Set();
    for (const child of pd.children) {
      for (const t of child.allTasksForCounts) {
        if (t.key) keys.add(t.key);
      }
      // Recurse into grandchildren etc.
      for (const k of collectDescendantPlanTaskKeys(child)) {
        keys.add(k);
      }
    }
    return keys;
  }

  // Render a plan and its children recursively.
  function renderPlanTree(pd, depth) {
    // Exclude tasks that belong to descendant plans from this plan's kanban.
    const descendantTaskKeys = collectDescendantPlanTaskKeys(pd);
    const ownTasks = descendantTaskKeys.size > 0
      ? pd.tasks.filter((t) => !descendantTaskKeys.has(t.key))
      : pd.tasks;
    const ownAllTasks = descendantTaskKeys.size > 0
      ? pd.allTasksForCounts.filter((t) => !descendantTaskKeys.has(t.key))
      : pd.allTasksForCounts;

    const visibleGlobalIndices = [];
    for (const vt of ownTasks) {
      const gi = _tasksByKey[vt.key];
      if (gi !== undefined) visibleGlobalIndices.push(gi);
    }

    // Aggregate counts across this plan and all descendant plans.
    const aggCounts = aggregatePlanCounts(pd);

    // Render child plans recursively.
    let childrenHtml = "";
    if (pd.children.length) {
      childrenHtml = `<div class="swimlane-children">`;
      for (const child of pd.children) {
        childrenHtml += renderPlanTree(child, depth + 1);
      }
      childrenHtml += `</div>`;
    }

    // Use de-duped own tasks for this swimlane's kanban.
    const dedupedPd = { ...pd, tasks: ownTasks, allTasksForCounts: ownAllTasks };
    return renderSwimlaneWithIndices(dedupedPd, visibleGlobalIndices, depth, childrenHtml, aggCounts);
  }

  // Recursively aggregate task counts across a plan and all its child plans.
  function aggregatePlanCounts(pd) {
    const counts = taskCounts(pd.allTasksForCounts);
    for (const child of pd.children) {
      const childCounts = aggregatePlanCounts(child);
      counts.total += childCounts.total;
      counts.pending += childCounts.pending;
      counts.in_progress += childCounts.in_progress;
      counts.complete += childCounts.complete;
      counts.blocked += childCounts.blocked;
      counts.superseded += childCounts.superseded;
      counts.done += childCounts.done;
    }
    return counts;
  }

  // Render swimlane using pre-mapped global indices for visible tasks.
  function renderSwimlaneWithIndices(planData, globalIndices, depth, childrenHtml, aggCounts) {
    const { title, planKey, tasks, allTasksForCounts, status, children } = planData;
    depth = depth || 0;
    childrenHtml = childrenHtml || "";
    const ownCounts = taskCounts(allTasksForCounts);
    const displayCounts = aggCounts || ownCounts;
    const expandKey = planKey || title;
    const isExpanded = _expandedPlans.has(expandKey);
    const planId = "plan-" + encodeURIComponent(expandKey).replace(/%/g, "_");
    const hasChildren = children && children.length > 0;
    const depthCls = depth > 0 ? " swimlane-nested swimlane-depth-" + Math.min(depth, 3) : "";

    let countBadges = "";
    if (displayCounts.in_progress) countBadges += `<span class="count-badge in_progress">${displayCounts.in_progress}</span>`;
    if (displayCounts.blocked) countBadges += `<span class="count-badge blocked">${displayCounts.blocked}</span>`;
    if (displayCounts.pending) countBadges += `<span class="count-badge pending">${displayCounts.pending}</span>`;
    if (hasChildren) countBadges += `<span class="count-badge child-plans">${children.length} sub-plan${children.length > 1 ? "s" : ""}</span>`;

    // Build mini kanban from visible tasks using global indices.
    const buckets = {};
    for (const col of BOARD_COLUMNS) buckets[col] = [];
    for (let i = 0; i < tasks.length; i++) {
      const gi = globalIndices[i];
      buckets[boardColumn(tasks[i].status)].push({ task: tasks[i], idx: gi });
    }

    const kanbanHtml = `<div class="columns-row columns-${BOARD_COLUMNS.length}">` +
      BOARD_COLUMNS.map((col) => {
        const items = buckets[col];
        const cardsHtml = items.length
          ? items.map(({ task, idx }) => renderCard(task, idx)).join("")
          : `<div class="column-empty">No tasks</div>`;
        return `
          <div class="column">
            <div class="column-header">
              <span class="column-dot ${col}"></span>
              <span class="column-name">${COLUMN_LABELS[col]}</span>
              <span class="column-count">${items.length}</span>
            </div>
            <div class="column-cards">${cardsHtml}</div>
          </div>`;
      }).join("") + `</div>`;

    return `
      <div class="swimlane${depthCls} ${isExpanded ? "" : "collapsed"}" id="${planId}" data-plan-key="${esc(expandKey)}">
        <div class="swimlane-header" onclick="AWM.togglePlan(this)">
          <span class="swimlane-toggle">&#9662;</span>
          <span class="swimlane-title">${esc(title)}</span>
          <div class="swimlane-counts">${countBadges}</div>
          ${renderProgress(displayCounts)}
        </div>
        <div class="swimlane-body">
          ${kanbanHtml}
          ${childrenHtml}
        </div>
      </div>`;
  }

  window.AWM = window.AWM || {};
  window.AWM.setScope = function (scope) {
    _boardScope = scope;
    document.querySelectorAll("[data-scope]").forEach((btn) => {
      btn.classList.toggle("active", btn.dataset.scope === scope);
    });
    loadBoard();
  };

  window.AWM.setFilter = function (filter) {
    _taskFilter = filter;
    document.querySelectorAll("[data-filter]").forEach((btn) => {
      btn.classList.toggle("active", btn.dataset.filter === filter);
    });
    loadBoard();
  };

  window.AWM.togglePlan = function (headerEl) {
    const swimlane = headerEl.closest(".swimlane");
    if (!swimlane) return;
    const key = swimlane.dataset.planKey || swimlane.querySelector(".swimlane-title")?.textContent || "";
    if (_expandedPlans.has(key)) {
      _expandedPlans.delete(key);
      swimlane.classList.add("collapsed");
    } else {
      _expandedPlans.add(key);
      swimlane.classList.remove("collapsed");
    }
  };

  // ---------------------------------------------------------------------------
  // Status Page
  // ---------------------------------------------------------------------------

  function isStatusPage() {
    return location.pathname === "/status.html";
  }

  function badgeClass(status) {
    if (status === "ok") return "ok";
    if (status === "warn" || status === "warning") return "warn";
    return "error";
  }

  function summaryStatus(summary) {
    // summary.ready is a boolean; missing_count/warning_count are ints.
    if (!summary.ready) return "error";
    if (summary.warning_count > 0 || summary.missing_count > 0) return "warn";
    return "ok";
  }

  function sourceStatus(s) {
    if (s.loaded) return "ok";
    if (s.exists) return "warn";
    return "missing";
  }

  function renderStatusPage(data) {
    const summary = data.summary || {};
    const project = data.project || {};
    const sources = data.sources || [];
    const integrations = data.integrations || [];
    const warnings = data.warnings || [];
    const missing = data.missing || [];

    const overallStatus = summaryStatus(summary);

    // Project info section
    const projectHtml = `
      <div class="status-section">
        <div class="status-section-header">
          <h2>Project</h2>
          <span class="status-badge ${badgeClass(overallStatus)}">${esc(overallStatus)}</span>
        </div>
        <div class="status-section-body">
          <div class="kv-row">
            <span class="kv-label">Project ID</span>
            <span class="kv-value">${esc(project.project_id || "\u2014")}</span>
          </div>
          <div class="kv-row">
            <span class="kv-label">Project Root</span>
            <span class="kv-value">${esc(project.project_root || "\u2014")}</span>
          </div>
          <div class="kv-row">
            <span class="kv-label">Backend</span>
            <span class="kv-value">${esc(project.backend || "\u2014")}</span>
          </div>
        </div>
      </div>`;

    // Sources section — real fields: kind, source_path, exists, loaded, item_count
    let sourcesBody = "";
    if (sources.length) {
      sourcesBody = sources
        .map(
          (s) => `
          <div class="source-item">
            <span class="source-name">${esc(s.kind || "unknown")}</span>
            <span class="source-path">${esc(s.source_path || s.absolute_path || "")}</span>
            <span class="source-status">
              <span class="status-badge ${badgeClass(sourceStatus(s))}">${s.loaded ? "loaded (" + (s.item_count || 0) + ")" : s.exists ? "not loaded" : "missing"}</span>
            </span>
          </div>`
        )
        .join("");
    } else {
      sourcesBody = `<div class="empty-message">No sources configured.</div>`;
    }
    const sourcesHtml = `
      <div class="status-section">
        <div class="status-section-header"><h2>Sources</h2></div>
        <div class="status-section-body">${sourcesBody}</div>
      </div>`;

    // Integrations section — real fields: id, summary, installed, present_targets, expected_targets, missing_targets
    let intBody = "";
    if (integrations.length) {
      intBody = integrations
        .map((s) => {
          let badge, cls, tooltipLines;
          if (s.installed) {
            badge = "installed";
            cls = "ok";
            tooltipLines = [s.present_targets + "/" + s.expected_targets + " targets present"];
          } else if (s.present_targets === 0) {
            badge = "not installed";
            cls = "";
            tooltipLines = [s.expected_targets + " target" + (s.expected_targets !== 1 ? "s" : "") + " required"];
            if (s.missing_targets && s.missing_targets.length) {
              tooltipLines.push(...s.missing_targets);
            }
          } else {
            badge = "not installed";
            cls = "";
            tooltipLines = [s.present_targets + "/" + s.expected_targets + " targets present"];
            if (s.missing_targets && s.missing_targets.length) {
              tooltipLines.push("Missing:");
              tooltipLines.push(...s.missing_targets);
            }
          }

          const tooltipHtml = tooltipLines.map((l) => esc(l)).join("\n");

          return `
            <div class="source-item">
              <span class="source-name">${esc(s.id)}</span>
              <span class="source-path">${esc(s.summary || "")}</span>
              <span class="source-status">
                <span class="tooltip-wrap">
                  <span class="status-badge ${cls}">${esc(badge)}</span>
                  <span class="tooltip-content">${tooltipHtml}</span>
                </span>
              </span>
            </div>`;
        })
        .join("");
    } else {
      intBody = `<div class="empty-message">No integrations available.</div>`;
    }
    const intHtml = `
      <div class="status-section">
        <div class="status-section-header"><h2>Integrations</h2></div>
        <div class="status-section-body">${intBody}</div>
      </div>`;

    // Warnings section
    const allWarnings = [
      ...warnings.map((w) => ({ text: typeof w === "string" ? w : w.message || w.detail || JSON.stringify(w), type: "warn" })),
      ...missing.map((m) => ({ text: typeof m === "string" ? m : m.message || m.detail || JSON.stringify(m), type: "missing" })),
    ];

    let warnBody = "";
    if (allWarnings.length) {
      warnBody = allWarnings
        .map(
          (w) => `
          <div class="warning-item">
            <span class="warning-icon">&#9888;</span>
            <span class="warning-text">${esc(w.text)}</span>
          </div>`
        )
        .join("");
    } else {
      warnBody = `<div class="empty-message">No warnings. Everything looks good.</div>`;
    }
    const warnHtml = `
      <div class="status-section">
        <div class="status-section-header"><h2>Warnings</h2></div>
        <div class="status-section-body">${warnBody}</div>
      </div>`;

    return projectHtml + sourcesHtml + intHtml + warnHtml;
  }

  async function loadStatus() {
    const container = document.getElementById("status-container");
    if (!container) return;

    try {
      const data = await getStatus();
      container.innerHTML = renderStatusPage(data);
    } catch (err) {
      console.error("Status load error:", err);
      container.innerHTML = `<div class="error-banner">Failed to load status: ${esc(err.message)}</div>`;
    }
  }

  // ---------------------------------------------------------------------------
  // Card Detail Modal
  // ---------------------------------------------------------------------------

  window.AWM = window.AWM || {};

  function ensureModalContainer() {
    let overlay = document.getElementById("card-modal-overlay");
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.id = "card-modal-overlay";
      overlay.className = "modal-overlay";
      overlay.innerHTML = `<div class="modal-content" id="card-modal-content"></div>`;
      overlay.addEventListener("click", function (e) {
        if (e.target === overlay) AWM.closeCard();
      });
      document.body.appendChild(overlay);
    }
    return overlay;
  }

  // Collect all descendants of a task recursively.
  function collectDescendants(taskKey) {
    const result = [];
    const queue = _childrenOf[taskKey] || [];
    const visited = new Set();
    for (let i = 0; i < queue.length; i++) {
      const ci = queue[i];
      if (visited.has(ci)) continue;
      visited.add(ci);
      result.push(ci);
      const child = _boardTasks[ci];
      if (child && _childrenOf[child.key]) {
        queue.push(..._childrenOf[child.key]);
      }
    }
    return result;
  }

  // Build a rollup summary for a parent task from all descendants.
  function buildRollup(taskKey) {
    const descIndices = collectDescendants(taskKey);
    if (!descIndices.length) return null;

    const descendants = descIndices.map((i) => _boardTasks[i]);
    const byStatus = {};
    for (const s of STATUSES) byStatus[s] = [];
    for (const d of descendants) {
      const s = STATUSES.includes(d.status) ? d.status : "pending";
      byStatus[s].push(d);
    }

    const total = descendants.length;
    const done = byStatus.complete.length + byStatus.superseded.length;
    const blocked = byStatus.blocked.length;
    const inProgress = byStatus.in_progress.length;
    const pending = total - done - blocked - inProgress;

    // Collect all outstanding acceptance criteria from incomplete leaves.
    const outstandingAC = [];
    const blockers = [];
    for (const d of descendants) {
      if (d.status === "complete" || d.status === "superseded") continue;
      if (d.blocked_reason) {
        blockers.push({ task: d, reason: d.blocked_reason });
      }
      if (d.acceptance_criteria && d.acceptance_criteria.length) {
        outstandingAC.push({ task: d, criteria: d.acceptance_criteria });
      }
    }

    return { total, done, blocked, inProgress, pending, byStatus, outstandingAC, blockers, descendants };
  }

  function renderRollup(rollup) {
    let html = "";

    // Progress bar
    const pctDone = Math.round((rollup.done / rollup.total) * 100);
    html += `<div class="rollup-progress">
      <div class="rollup-bar">
        <div class="rollup-bar-fill" style="width:${pctDone}%"></div>
      </div>
      <span class="rollup-pct">${rollup.done}/${rollup.total} complete (${pctDone}%)</span>
    </div>`;

    // Status breakdown
    const breakdown = [];
    if (rollup.inProgress) breakdown.push(`<span class="count-badge in_progress">${rollup.inProgress} In Progress</span>`);
    if (rollup.pending) breakdown.push(`<span class="count-badge pending">${rollup.pending} Pending</span>`);
    if (rollup.blocked) breakdown.push(`<span class="count-badge blocked">${rollup.blocked} Blocked</span>`);
    if (rollup.done) breakdown.push(`<span class="count-badge complete">${rollup.done} Done</span>`);
    if (breakdown.length) {
      html += `<div class="rollup-breakdown">${breakdown.join(" ")}</div>`;
    }

    // Blockers
    if (rollup.blockers.length) {
      const items = rollup.blockers.map((b) => {
        const idx = _tasksByKey[b.task.key];
        const link = idx !== undefined ? taskLink(b.task.key) : esc(b.task.summary || b.task.key);
        return `<li class="rollup-blocker"><span class="warning-icon">&#9888;</span> ${link}: ${esc(b.reason)}</li>`;
      }).join("");
      html += `<div class="modal-field">
        <span class="modal-label">Blockers</span>
        <ul class="modal-nav-list">${items}</ul>
      </div>`;
    }

    // Outstanding acceptance criteria from incomplete tasks
    if (rollup.outstandingAC.length) {
      const groups = rollup.outstandingAC.map((g) => {
        const idx = _tasksByKey[g.task.key];
        const link = idx !== undefined ? taskLink(g.task.key) : esc(g.task.summary || g.task.key);
        const criteria = g.criteria.map((c) => `<li>${esc(c)}</li>`).join("");
        return `<div class="rollup-ac-group">
          <div class="rollup-ac-task">${link}</div>
          <ul class="modal-list">${criteria}</ul>
        </div>`;
      }).join("");
      html += `<div class="modal-field">
        <span class="modal-label">Outstanding Acceptance Criteria</span>
        ${groups}
      </div>`;
    }

    return html;
  }

  function taskLink(key) {
    const idx = _tasksByKey[key];
    if (idx === undefined) return `<code>${esc(key)}</code>`;
    const t = _boardTasks[idx];
    const type = taskType(key);
    const typeLabel = type ? `<span class="card-type ${type.cls}" style="font-size:0.68rem">${esc(type.label)}</span> ` : "";
    return `<a class="task-link" onclick="AWM.openCard(${idx})">${typeLabel}${esc(t.summary || key)}</a>`;
  }

  window.AWM.openCard = function (idx) {
    const task = _boardTasks[idx];
    if (!task) return;

    const overlay = ensureModalContainer();
    const content = document.getElementById("card-modal-content");
    const statusCls = STATUSES.includes(task.status) ? task.status : "pending";
    const statusLabel = STATUS_LABELS[task.status] || task.status;
    const type = taskType(task.key);

    let sections = "";

    // Type + Status row
    sections += `<div class="modal-status">`;
    if (type) {
      sections += `<span class="card-type ${type.cls}">${esc(type.label)}</span>`;
    }
    sections += `<span class="count-badge ${statusCls}">${esc(statusLabel)}</span>`;
    sections += `</div>`;

    // Key
    sections += `<div class="modal-field">
      <span class="modal-label">Key</span>
      <span class="modal-value mono">${esc(task.key || "")}</span>
    </div>`;

    // Plan
    if (task._planTitle) {
      sections += `<div class="modal-field">
        <span class="modal-label">Plan</span>
        <span class="modal-value">${esc(task._planTitle)}</span>
      </div>`;
    }

    // Parent (navigable link)
    if (task.parent_task_key) {
      sections += `<div class="modal-field">
        <span class="modal-label">Parent</span>
        <span class="modal-value">${taskLink(task.parent_task_key)}</span>
      </div>`;
    }

    // Children (navigable links)
    const children = _childrenOf[task.key] || [];
    if (children.length) {
      const childLinks = children.map((ci) => {
        const child = _boardTasks[ci];
        const childStatusCls = STATUSES.includes(child.status) ? child.status : "pending";
        return `<li><span class="column-dot ${childStatusCls}" style="width:7px;height:7px;display:inline-block"></span> ${taskLink(child.key)}</li>`;
      }).join("");
      sections += `<div class="modal-field">
        <span class="modal-label">Children</span>
        <ul class="modal-nav-list">${childLinks}</ul>
      </div>`;
    }

    // Rollup — hydrate parent tasks with descendant data
    const rollup = buildRollup(task.key);
    if (rollup) {
      sections += `<div class="modal-field">
        <span class="modal-label">Progress</span>
        ${renderRollup(rollup)}
      </div>`;
    }

    // Blocked reason
    if (task.blocked_reason) {
      sections += `<div class="modal-field">
        <span class="modal-label">Blocked</span>
        <span class="modal-value warn">${esc(task.blocked_reason)}</span>
      </div>`;
    }

    // Dependencies (navigable links)
    if (task.depends_on && task.depends_on.length) {
      const depLinks = task.depends_on.map((d) => `<li>${taskLink(d)}</li>`).join("");
      sections += `<div class="modal-field">
        <span class="modal-label">Depends on</span>
        <ul class="modal-nav-list">${depLinks}</ul>
      </div>`;
    }

    // Acceptance criteria
    if (task.acceptance_criteria && task.acceptance_criteria.length) {
      const items = task.acceptance_criteria
        .map((c) => `<li>${esc(c)}</li>`)
        .join("");
      sections += `<div class="modal-field">
        <span class="modal-label">Acceptance Criteria</span>
        <ul class="modal-list">${items}</ul>
      </div>`;
    }

    // References
    if (task.references && task.references.length) {
      const refs = task.references
        .map((r) => `<li><code>${esc(r)}</code></li>`)
        .join("");
      sections += `<div class="modal-field">
        <span class="modal-label">References</span>
        <ul class="modal-list">${refs}</ul>
      </div>`;
    }

    content.innerHTML = `
      <button class="modal-close" onclick="AWM.closeCard()">&times;</button>
      <div class="modal-title">${esc(task.summary || task.key || "")}</div>
      <div class="modal-body">${sections}</div>`;

    overlay.classList.add("open");
    document.addEventListener("keydown", _modalEscHandler);
  };

  window.AWM.closeCard = function () {
    const overlay = document.getElementById("card-modal-overlay");
    if (overlay) overlay.classList.remove("open");
    document.removeEventListener("keydown", _modalEscHandler);
  };

  function _modalEscHandler(e) {
    if (e.key === "Escape") AWM.closeCard();
  }

  // ---------------------------------------------------------------------------
  // Polling
  // ---------------------------------------------------------------------------

  function startPolling(fn, intervalMs) {
    // Initial load
    fn();
    // Repeat
    setInterval(fn, intervalMs);
  }

  // ---------------------------------------------------------------------------
  // Init
  // ---------------------------------------------------------------------------

  document.addEventListener("DOMContentLoaded", function () {
    if (isboardPage()) {
      startPolling(loadBoard, 10000);
    } else if (isStatusPage()) {
      startPolling(loadStatus, 30000);
    }
  });
})();
