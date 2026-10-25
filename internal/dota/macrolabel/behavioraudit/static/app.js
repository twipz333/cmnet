(() => {
  let cases = [];
  let idx = 0;
  let current = null;
  let actions = [];
  let mode = "primary"; // primary | reject | ambiguous | unlabeled
  let mapInfo = null;
  let calibration = null;
  let mapOpacity = 1;
  let showCalibration = false;

  const $ = (id) => document.getElementById(id);

  function transform() {
    return (mapInfo && mapInfo.transform) || null;
  }

  function worldToPixel(x, y) {
    const t = transform();
    if (!t || !t.scale) {
      // Fallback: approximate legacy square (not used when map available).
      const W = 16000;
      return { x: x + 8000, y: -y + 8000, w: W, h: W };
    }
    return {
      x: (x - t.pos_x) / t.scale,
      y: (t.pos_y - y) / t.scale,
      w: t.width || 1024,
      h: t.height || 1024,
    };
  }

  function segmentRole(gameTime, c) {
    if (gameTime < c.start_game_time) return "BEFORE";
    if (gameTime > c.end_game_time) return "AFTER";
    return "AUTO";
  }

  async function init() {
    const [sess, list, acts, map] = await Promise.all([
      fetch("/api/session").then((r) => r.json()),
      fetch("/api/cases").then((r) => r.json()),
      fetch("/api/actions").then((r) => r.json()),
      fetch("/api/map").then((r) => r.json()),
    ]);
    cases = list;
    actions = acts;
    mapInfo = map;
    applyMapChrome(sess);
    $("progress").textContent = `${sess.reviewed}/${sess.total}`;
    if (sess.macro_schema_warning) {
      console.warn(sess.macro_schema_warning);
      const badge = $("map-badge");
      if (badge) badge.title = (badge.title || "") + " | " + sess.macro_schema_warning;
    }
    idx = Math.max(0, cases.findIndex((c) => !c.review_status));
    if (idx < 0) idx = 0;

    $("map-opacity").onchange = (e) => {
      mapOpacity = parseFloat(e.target.value);
      applyOpacity();
      if (current) drawMap(current);
    };
    $("map-calibrate").onchange = async (e) => {
      showCalibration = !!e.target.checked;
      if (showCalibration && !calibration) {
        try {
          calibration = await fetch("/api/map/calibration").then((r) => r.json());
        } catch (_) {
          calibration = { available: false };
        }
      }
      if (current) drawMap(current);
    };

    await loadCase();
    $("prev").onclick = () => move(-1);
    $("next").onclick = () => move(1);
    window.addEventListener("keydown", onKey);
  }

  function applyMapChrome(sess) {
    const m = (sess && sess.map) || mapInfo || {};
    mapInfo = m;
    const badge = $("map-badge");
    const geo = m.geometry_version || (sess && sess.geometry_version) || "?";
    const fallback = $("map-fallback");
    const img = $("minimap-bg");
    if (m.available) {
      badge.textContent = `Map OK · ${geo}`;
      badge.className = "map-badge ok";
      if (m.warning) {
        badge.textContent = `Map warn · ${geo}`;
        badge.className = "map-badge warn";
        badge.title = m.warning;
      } else {
        badge.title = m.meta ? `build ${m.meta.client_build || "?"} · ${m.meta.source || ""}` : "";
      }
      fallback.classList.add("hidden");
      img.src = "/api/map/image";
      img.style.display = "";
    } else {
      badge.textContent = `Map missing · ${geo}`;
      badge.className = "map-badge bad";
      badge.title = m.warning || m.error || "";
      fallback.classList.remove("hidden");
      img.removeAttribute("src");
      img.style.display = "none";
    }
    applyOpacity();
  }

  function applyOpacity() {
    const img = $("minimap-bg");
    const dim = $("map-dim");
    img.style.opacity = String(mapOpacity);
    dim.style.opacity = mapOpacity === 0 ? "0" : "1";
    dim.style.background = `rgba(0,0,0,${0.42 * mapOpacity})`;
  }

  async function move(d) {
    idx = Math.max(0, Math.min(cases.length - 1, idx + d));
    await loadCase();
  }

  async function loadCase(reveal) {
    const id = cases[idx].id;
    const url = reveal ? `/api/cases/${id}?reveal=1` : `/api/cases/${id}`;
    current = await fetch(url).then((r) => r.json());
    mode = "primary";
    render();
    const sess = await fetch("/api/session").then((r) => r.json());
    $("progress").textContent = `${sess.reviewed}/${sess.total}`;
    applyMapChrome(sess);
  }

  function render() {
    const c = current;
    const actionLabel = c.auto_action_name
      || (c.auto_action ? actionDisplayName(c.auto_action) : c.auto_status);
    const schemaWarn = c.macro_schema_warning
      ? `<div class="schema-warn">⚠ ${escapeHtml(c.macro_schema_warning)}</div>`
      : "";
    $("meta").innerHTML = `
      <div><strong>#${idx + 1}</strong> · ${c.auto_status} · t=${c.start_game_time}…${c.end_game_time}
      (${(c.duration_seconds || 0).toFixed(1)}s) · ${c.side || ""}</div>
      <div>Авто: ${escapeHtml(actionLabel)}${c.auto_confidence != null ? ` · conf=${c.auto_confidence.toFixed(3)}` : ""}</div>
      ${c.ambiguity_reason ? `<div>Ambiguity: ${escapeHtml(String(c.ambiguity_reason))}</div>` : ""}
      ${schemaWarn}
    `;
    $("definition").textContent = c.auto_action_description || c.definition
      || "Нет авто-действия — оцените, видно ли ясное macro-поведение.";
    const ev = c.evidence_summary || {};
    const types = (ev.types || []).join(", ") || "—";
    const descs = (ev.descriptions || []).slice(0, 4).map((d) => `• ${d}`).join("<br>");
    let group = "";
    if (c.grouping_summary) {
      group = `grouped=${c.grouping_summary.grouped_ratio.toFixed(2)} spread=${c.grouping_summary.spread.toFixed(0)} region=${c.grouping_summary.main_group_region || "—"}`;
    }
    $("evidence").innerHTML = `<strong>Evidence:</strong> ${types}<br>${descs}${group ? `<br>${group}` : ""}`;

    drawMap(c);
    drawTimeline(c);

    const details = {
      auto_action: c.auto_action
        ? { id: c.auto_action.id, name: c.auto_action_name || actionDisplayName(c.auto_action) }
        : null,
      competing: (c.competing || []).map(devCompeting),
      rejected_candidates: c.reveal_alts
        ? (c.rejected_candidates || []).map(devRejected)
        : "(скрыто до НЕТ)",
      lane_summary: c.lane_summary,
      smoke_anchor_id: c.smoke_anchor_id,
      auto_confidence: c.auto_confidence,
      macro_action_schema_version: c.macro_action_schema_version,
    };
    $("details-pre").textContent = JSON.stringify(details, null, 2);

    renderActions();
  }

  function drawTimeline(c) {
    const el = $("timeline");
    el.innerHTML = "";
    (c.timeline_markers || []).forEach((m) => {
      const s = document.createElement("span");
      s.className = m.role;
      s.textContent = m.role;
      el.appendChild(s);
    });
  }

  function svgEl(name, attrs) {
    const el = document.createElementNS("http://www.w3.org/2000/svg", name);
    Object.entries(attrs || {}).forEach(([k, v]) => el.setAttribute(k, String(v)));
    return el;
  }

  function drawMap(c) {
    const svg = $("map");
    svg.innerHTML = "";
    const t = transform();
    const W = (t && t.width) || 1024;
    const H = (t && t.height) || 1024;
    svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
    svg.setAttribute("preserveAspectRatio", "xMidYMid meet");

    if (!t) {
      const bg = svgEl("rect", { x: 0, y: 0, width: W, height: H, fill: "#0b1015" });
      svg.appendChild(bg);
    }

    // Region labels (coarse) — world→pixel
    (c.map_regions || []).forEach((r) => {
      if (r.cx == null) return;
      const p = worldToPixel(r.cx, r.cy);
      const text = svgEl("text", {
        x: p.x, y: p.y, fill: "rgba(200,210,220,.55)",
        "font-size": Math.max(14, W * 0.028), "text-anchor": "middle",
      });
      text.textContent = r.label || r.id;
      svg.appendChild(text);
    });

    const colors = ["#3d9cf0", "#3cb371", "#e0b44e", "#c084fc", "#f472b6"];
    const byHero = {};
    (c.trajectories || []).forEach((p) => {
      if (!byHero[p.hero]) byHero[p.hero] = [];
      byHero[p.hero].push(p);
    });
    let ci = 0;
    Object.keys(byHero).sort().forEach((hero) => {
      const pts = byHero[hero].slice().sort((a, b) => a.game_time - b.game_time);
      const color = colors[ci % colors.length];
      drawTrajectory(svg, pts, color, c, hero, W);
      ci++;
    });

    (c.enemy_markers || []).forEach((p) => {
      const pix = worldToPixel(p.x, p.y);
      const r = Math.max(4, W * 0.008);
      if (p.observed) {
        svg.appendChild(svgEl("circle", {
          cx: pix.x, cy: pix.y, r, fill: "#e35d6a", stroke: "#fff", "stroke-width": 1.2,
        }));
      } else {
        svg.appendChild(svgEl("circle", {
          cx: pix.x, cy: pix.y, r,
          fill: "none", stroke: "#9ca3af", "stroke-width": 1.5, "stroke-dasharray": "3 2",
          opacity: 0.85,
        }));
      }
    });

    if (showCalibration) drawCalibration(svg, W, H);
  }

  function drawTrajectory(svg, pts, color, c, hero, W) {
    if (!pts.length) return;
    const roles = { BEFORE: { op: 0.35, dash: "6 4" }, AUTO: { op: 0.95, dash: "" }, AFTER: { op: 0.55, dash: "2 3" } };

    // Split into contiguous role segments
    let i = 0;
    while (i < pts.length) {
      const role = segmentRole(pts[i].game_time, c);
      let j = i + 1;
      while (j < pts.length && segmentRole(pts[j].game_time, c) === role) j++;
      const seg = pts.slice(i, j);
      const style = roles[role] || roles.AUTO;
      if (seg.length >= 2) {
        const pl = svgEl("polyline", {
          points: seg.map((p) => {
            const q = worldToPixel(p.x, p.y);
            return `${q.x},${q.y}`;
          }).join(" "),
          fill: "none",
          stroke: color,
          "stroke-width": Math.max(2, W * 0.0045),
          "stroke-linecap": "round",
          "stroke-linejoin": "round",
          opacity: style.op,
        });
        if (style.dash) pl.setAttribute("stroke-dasharray", style.dash);
        svg.appendChild(pl);
      }
      i = j;
    }

    const start = worldToPixel(pts[0].x, pts[0].y);
    const end = worldToPixel(pts[pts.length - 1].x, pts[pts.length - 1].y);
    const r0 = Math.max(3, W * 0.006);
    const r1 = Math.max(5, W * 0.011);
    svg.appendChild(svgEl("circle", { cx: start.x, cy: start.y, r: r0, fill: color, opacity: 0.4 }));
    svg.appendChild(svgEl("circle", { cx: end.x, cy: end.y, r: r1, fill: color, opacity: 1, stroke: "#fff", "stroke-width": 1 }));

    // Arrow at end
    if (pts.length >= 2) {
      const prev = worldToPixel(pts[pts.length - 2].x, pts[pts.length - 2].y);
      const ang = Math.atan2(end.y - prev.y, end.x - prev.x);
      const len = Math.max(8, W * 0.018);
      const a1 = ang + Math.PI * 0.82;
      const a2 = ang - Math.PI * 0.82;
      const arrow = svgEl("polygon", {
        points: [
          `${end.x},${end.y}`,
          `${end.x + Math.cos(a1) * len},${end.y + Math.sin(a1) * len}`,
          `${end.x + Math.cos(a2) * len},${end.y + Math.sin(a2) * len}`,
        ].join(" "),
        fill: color,
      });
      svg.appendChild(arrow);
    }

    const label = hero.replace(/^npc_dota_hero_/, "");
    const text = svgEl("text", {
      x: end.x + r1 + 2, y: end.y - 2,
      fill: "#e7ecf1", "font-size": Math.max(11, W * 0.022),
      "paint-order": "stroke", stroke: "rgba(0,0,0,.75)", "stroke-width": 3,
    });
    text.textContent = label;
    svg.appendChild(text);
  }

  function drawCalibration(svg, W, H) {
    // Axes
    svg.appendChild(svgEl("line", { x1: 0, y1: H / 2, x2: W, y2: H / 2, stroke: "rgba(255,255,255,.25)", "stroke-width": 1 }));
    svg.appendChild(svgEl("line", { x1: W / 2, y1: 0, x2: W / 2, y2: H, stroke: "rgba(255,255,255,.25)", "stroke-width": 1 }));

    const anchors = (calibration && calibration.report && calibration.report.anchors) || [];
    anchors.forEach((a) => {
      const x = a.pixel.x != null ? a.pixel.x : a.pixel.X;
      const y = a.pixel.y != null ? a.pixel.y : a.pixel.Y;
      svg.appendChild(svgEl("circle", {
        cx: x, cy: y, r: Math.max(5, W * 0.01),
        fill: "none", stroke: "#fbbf24", "stroke-width": 2,
      }));
      const t = svgEl("text", {
        x: x + 6, y: y - 6, fill: "#fbbf24",
        "font-size": Math.max(10, W * 0.02),
      });
      t.textContent = a.id;
      svg.appendChild(t);
    });

    const regions = (calibration && calibration.regions) || [];
    regions.forEach((reg) => {
      const pts = (reg.pixels || []).map((p) => `${p.x},${p.y}`).join(" ");
      if (!pts) return;
      svg.appendChild(svgEl("polygon", {
        points: pts,
        fill: "rgba(61,156,240,.08)",
        stroke: "rgba(61,156,240,.35)",
        "stroke-width": 1,
      }));
    });
  }

  function renderActions() {
    const c = current;
    const q = $("question");
    const primary = $("primary-btns");
    const secondary = $("secondary-btns");
    const picker = $("picker");
    const help = $("candidate-help");
    primary.innerHTML = "";
    secondary.innerHTML = "";
    picker.classList.add("hidden");
    picker.innerHTML = "";
    if (help) help.innerHTML = "";

    if (mode === "reject") {
      q.textContent = "Что ближе к фактическому поведению?";
      renderRejectedHelp(c);
      addBtn(primary, "Ни один не подходит", "no", () => submit({
        status: "REJECTED", accepted_auto: false, reason: "TAXONOMY_GAP", confidence: "MEDIUM"
      }));
      addBtn(primary, "Несколько подходят", "warn", () => submit({
        status: "REJECTED", accepted_auto: false, reason: "TAXONOMY_OVERLAP", confidence: "MEDIUM"
      }));
      addBtn(primary, "Непонятно", "warn", () => submit({
        status: "UNCLEAR", accepted_auto: false, reason: "UNCLEAR_OBSERVABILITY", confidence: "LOW"
      }));
      showPicker((id) => submit({
        status: "REJECTED", accepted_auto: false, human_action_id: id, confidence: "MEDIUM"
      }));
      return;
    }

    if (c.auto_status === "AMBIGUOUS") {
      q.textContent = "Какой вариант описывает фактическое поведение?";
      const a = (c.competing || [])[0];
      const b = (c.competing || [])[1];
      renderCompetingHelp(a, b);
      if (a) addBtn(primary, `A: ${candidateName(a)}`, "ok", () => submit({
        status: "CONFIRMED", accepted_auto: false, human_action_id: a.action_id, confidence: "MEDIUM"
      }));
      if (b) addBtn(primary, `B: ${candidateName(b)}`, "ok", () => submit({
        status: "CONFIRMED", accepted_auto: false, human_action_id: b.action_id, confidence: "MEDIUM"
      }));
      addBtn(primary, "Оба подходят", "warn", () => submit({ status: "BOTH_OK", accepted_auto: false, confidence: "MEDIUM" }));
      addBtn(primary, "Ни один", "no", () => submit({ status: "NEITHER", accepted_auto: false, reason: "TAXONOMY_GAP", confidence: "MEDIUM" }));
      addBtn(primary, "Непонятно", "warn", () => submit({ status: "UNCLEAR", accepted_auto: false, confidence: "LOW" }));
      return;
    }

    if (c.auto_status === "UNLABELED") {
      q.textContent = "Видно ли ясное macro-поведение команды?";
      addBtn(primary, "ДА — выбрать", "ok", () => {
        mode = "reject";
        q.textContent = "Выберите действие";
        primary.innerHTML = "";
        showPicker((id) => submit({
          status: "CONFIRMED", accepted_auto: false, human_action_id: id, confidence: "MEDIUM"
        }));
        addBtn(secondary, "Отмена", "", () => { mode = "primary"; renderActions(); });
      });
      addBtn(primary, "Нет ясного действия", "no", () => submit({
        status: "NO_CLEAR_ACTION", accepted_auto: true, confidence: "MEDIUM"
      }));
      addBtn(primary, "Непонятно", "warn", () => submit({
        status: "UNCLEAR", accepted_auto: false, confidence: "LOW"
      }));
      return;
    }

    q.textContent = "Описывает ли это фактическое поведение команды?";
    addBtn(primary, "ДА", "ok", () => submit({
      status: "CONFIRMED", accepted_auto: true, confidence: "HIGH",
      human_action_id: c.auto_action ? c.auto_action.id : undefined
    }));
    addBtn(primary, "НЕТ", "no", async () => {
      mode = "reject";
      await loadCase(true);
      mode = "reject";
      renderActions();
    });
    addBtn(primary, "НЕПОНЯТНО", "warn", () => submit({
      status: "UNCLEAR", accepted_auto: false, confidence: "LOW"
    }));
  }

  function showPicker(onPick) {
    const picker = $("picker");
    picker.classList.remove("hidden");
    actions.forEach((a) => {
      const b = document.createElement("button");
      const name = a.action_name || a.label;
      b.textContent = name;
      b.title = a.action_description || a.definition || "";
      b.onclick = () => onPick(a.id);
      picker.appendChild(b);
    });
  }

  function candidateName(c) {
    if (!c) return "UNKNOWN";
    return c.action_name || ("UNKNOWN ACTION (id=" + c.action_id + ")");
  }

  function candidateDesc(c) {
    return (c && (c.action_description || "")) || "";
  }

  function renderCompetingHelp(a, b) {
    const help = $("candidate-help");
    if (!help) return;
    const parts = [];
    if (a) {
      parts.push(`<div class="cand"><strong>A: ${escapeHtml(candidateName(a))}</strong>`
        + (candidateDesc(a) ? `<div class="desc">${escapeHtml(candidateDesc(a))}</div>` : "")
        + `</div>`);
    }
    if (b) {
      parts.push(`<div class="cand"><strong>B: ${escapeHtml(candidateName(b))}</strong>`
        + (candidateDesc(b) ? `<div class="desc">${escapeHtml(candidateDesc(b))}</div>` : "")
        + `</div>`);
    }
    help.innerHTML = parts.join("");
  }

  function renderRejectedHelp(c) {
    const help = $("candidate-help");
    if (!help) return;
    const rows = (c.rejected_candidates || []);
    if (!rows.length) {
      help.innerHTML = "";
      return;
    }
    const body = rows.map((r) => {
      const name = candidateName(r);
      const conf = (r.confidence != null) ? Number(r.confidence).toFixed(2) : "";
      return `<div class="rej-row"><span>${escapeHtml(name)}</span><span>${conf}</span></div>`;
    }).join("");
    help.innerHTML = `<div class="cand"><strong>Rejected candidates</strong>${body}</div>`;
  }

  function actionDisplayName(a) {
    if (!a) return "";
    if (a.target && a.target !== "NONE") return `${a.kind} ${a.target}`;
    return a.kind || "";
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function devCompeting(c) {
    return {
      action_id: c.action_id,
      action_name: c.action_name,
      confidence: c.confidence,
      score_gap: c.score_gap,
      unknown: c.unknown,
    };
  }

  function devRejected(c) {
    return {
      action_id: c.action_id,
      action_name: c.action_name,
      confidence: c.confidence,
      reason: c.reason,
      unknown: c.unknown,
    };
  }

  function addBtn(parent, label, cls, fn) {
    const b = document.createElement("button");
    b.textContent = label;
    if (cls) b.className = cls;
    b.onclick = fn;
    parent.appendChild(b);
  }

  async function submit(review) {
    const id = current.id;
    await fetch(`/api/cases/${id}/review`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(review),
    });
    cases[idx].review_status = review.status;
    if (idx < cases.length - 1) {
      idx++;
      await loadCase();
    } else {
      await loadCase();
      alert("Все кейсы в выборке просмотрены (или обновлены).");
    }
  }

  function onKey(e) {
    if (e.target && (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA" || e.target.tagName === "SELECT")) return;
    if (e.key === "ArrowLeft") { move(-1); return; }
    if (e.key === "ArrowRight") { move(1); return; }
    if (e.key === "s" || e.key === "S") {
      submit({ status: "SKIPPED", accepted_auto: false, confidence: "LOW" });
      return;
    }
    if (mode !== "primary") return;
    const st = current && current.auto_status;
    if (e.key === "1") {
      if (st === "LABELED") $("primary-btns").querySelector("button.ok")?.click();
      else if (st === "AMBIGUOUS") $("primary-btns").children[0]?.click();
      else if (st === "UNLABELED") $("primary-btns").children[0]?.click();
    }
    if (e.key === "2") {
      if (st === "LABELED") $("primary-btns").querySelector("button.no")?.click();
      else if (st === "AMBIGUOUS") $("primary-btns").children[1]?.click();
      else if (st === "UNLABELED") $("primary-btns").children[1]?.click();
    }
    if (e.key === "3") {
      if (st === "LABELED") $("primary-btns").querySelector("button.warn")?.click();
      else if (st === "AMBIGUOUS") $("primary-btns").children[2]?.click();
      else if (st === "UNLABELED") $("primary-btns").children[2]?.click();
    }
  }

  init().catch((err) => {
    document.body.innerHTML = `<pre style="color:#e35d6a;padding:2rem">${err}</pre>`;
  });
})();
