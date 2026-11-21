"use strict";

const $ = (id) => document.getElementById(id);
const sealable = new Set(["png", "jpeg", "webp", "svg", "pdf", "docx", "xlsx", "pptx", "zip"]);
const verdictText = { betrays: "Betrays you", whispers: "Whispers", silent: "Keeps silent" };
const formatName = {
  png: "PNG image", jpeg: "JPEG image", webp: "WebP image", svg: "SVG drawing", pdf: "PDF document",
  docx: "Word document", xlsx: "Excel workbook", pptx: "PowerPoint presentation", zip: "Zip archive",
};
const mime = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", webp: "image/webp", mp4: "video/mp4", txt: "text/plain", pdf: "application/pdf" };
const groups = {
  All: null,
  Images: ["png", "jpeg", "webp"],
  PDF: ["pdf"],
  Office: ["docx", "xlsx", "pptx"],
  SVG: ["svg"],
  Archives: ["zip"],
};
const maxSize = 512 * 1024 * 1024;

const results = [];

const ready = (async () => {
  const go = new Go();
  const res = await fetch("sphinx.wasm");
  const { instance } = await WebAssembly.instantiateStreaming(res, go.importObject);
  go.run(instance);
  renderRules("All");
})();

if ("serviceWorker" in navigator && (location.protocol === "https:" || location.hostname === "localhost")) {
  navigator.serviceWorker.register("sw.js").catch(() => {});
}

function el(tag, props = {}, ...children) {
  const node = Object.assign(document.createElement(tag), props);
  for (const c of children) if (c != null) node.append(c);
  return node;
}

function status(text) {
  $("status").textContent = text;
}

function download(name, data, type) {
  const a = el("a", { href: URL.createObjectURL(new Blob([data], { type })), download: name });
  document.body.append(a);
  a.click();
  a.remove();
}

async function examine(items) {
  try {
    await ready;
  } catch (e) {
    status("The inspector failed to load: " + e.message);
    return;
  }
  let done = 0;
  for (const { name, load, size } of items) {
    status(`The Sphinx is questioning ${name}… (${++done} of ${items.length})`);
    if (size > maxSize) {
      results.push({ name, error: "larger than 512 MB; use the command-line tool for files this big" });
      continue;
    }
    const bytes = await load();
    const raw = sphinx.inspect(bytes);
    if (typeof raw !== "string") {
      results.push({ name, error: raw.error });
      continue;
    }
    results.push({ name, bytes, report: JSON.parse(raw) });
    await new Promise((r) => setTimeout(r));
  }
  status("");
  renderResults();
  $("results").scrollIntoView({ behavior: "smooth", block: "start" });
}

function renderResults() {
  const box = $("cards");
  box.replaceChildren();
  $("results").hidden = results.length === 0;
  const count = { betrays: 0, whispers: 0, silent: 0, unreadable: 0 };
  const order = { betrays: 0, whispers: 1, silent: 2 };
  const sorted = [...results].sort((a, b) => (a.error ? 3 : order[a.report.verdict]) - (b.error ? 3 : order[b.report.verdict]));
  for (const r of sorted) {
    if (r.error) {
      count.unreadable++;
      box.append(el("details", { className: "card" },
        el("summary", {}, el("span", { className: "card-name", textContent: r.name }), el("span", { className: "pill pill-unreadable", textContent: "Unreadable" })),
        el("p", { className: "card-note", textContent: r.error })));
      continue;
    }
    count[r.report.verdict]++;
    box.append(card(r, results.length === 1 || r.report.verdict === "betrays"));
  }
  const parts = [];
  if (count.betrays) parts.push(`${count.betrays} betray${count.betrays === 1 ? "s" : ""} you`);
  if (count.whispers) parts.push(`${count.whispers} whisper${count.whispers === 1 ? "s" : ""}`);
  if (count.silent) parts.push(`${count.silent} keep${count.silent === 1 ? "s" : ""} silent`);
  if (count.unreadable) parts.push(`${count.unreadable} could not be read`);
  $("summary").textContent = `The Sphinx questioned ${results.length} file${results.length === 1 ? "" : "s"}: ${parts.join(", ")}.`;
}

function card(r, open) {
  const { report } = r;
  const node = el("details", { className: "card card-" + report.verdict, open });
  const count = report.findings.length;
  node.append(el("summary", {},
    el("span", { className: "card-name", textContent: r.name }),
    el("span", { className: "card-format", textContent: formatName[report.format] || report.format }),
    el("span", { className: "pill pill-" + report.verdict, textContent: verdictText[report.verdict] + (count ? ` · ${count}` : "") })));
  node.addEventListener("toggle", () => {
    if (node.open && !node.dataset.filled) fill(node, r);
  }, { once: false });
  if (open) fill(node, r);
  return node;
}

function fill(node, r) {
  node.dataset.filled = "1";
  const { report } = r;
  const list = el("ol", { className: "findings" });
  if (report.findings.length === 0) {
    list.append(el("li", { className: "finding" }, el("span", { className: "sev sev-low", textContent: "none" }),
      el("div", {}, el("p", { textContent: "Nothing found. That does not prove the file is safe, only that none of the questions found an answer." }))));
  }
  for (const f of report.findings) {
    list.append(el("li", { className: "finding" },
      el("span", { className: "sev sev-" + f.severity, textContent: f.severity }),
      el("div", {},
        el("h3", { textContent: f.title }),
        el("p", { textContent: f.message }),
        f.evidence ? el("p", { className: "evidence", textContent: f.evidence }) : null,
        el("details", {},
          el("summary", { textContent: "Why it matters and what to do" }),
          el("p", { textContent: f.why }),
          el("p", { textContent: f.fix }),
          el("p", { className: "rule-id", textContent: f.rule + (f.location ? " · " + f.location : "") })))));
  }
  node.append(list);
  if (sealable.has(report.format) && report.findings.length > 0) {
    const btn = el("button", { type: "button", className: "seal", textContent: "Download a cleaned copy" });
    const note = el("p", { className: "card-note" });
    btn.addEventListener("click", () => seal(r, note));
    node.append(el("div", { className: "card-actions" }, btn, note));
  }
  reveal(node, r);
}

function reveal(node, r) {
  const arts = sphinx.reveal(r.bytes);
  if (!Array.isArray(arts) || arts.length === 0) return;
  const box = el("div", { className: "artifacts" });
  const fmt = r.report.format;
  if (fmt === "png" || fmt === "jpeg" || fmt === "webp") {
    const url = URL.createObjectURL(new Blob([r.bytes], { type: "image/" + fmt }));
    box.append(el("div", { className: "artifact seen" }, el("p", { textContent: "What anyone opening the file sees" }), el("img", { src: url, alt: "the visible picture" })));
  }
  for (const a of arts) {
    const ext = a.name.split(".").pop().toLowerCase();
    const url = URL.createObjectURL(new Blob([a.data], { type: mime[ext] || "application/octet-stream" }));
    const art = el("div", { className: "artifact" }, el("p", { textContent: a.note }));
    if (ext === "png" || ext === "jpg" || ext === "jpeg" || ext === "webp") {
      art.append(el("img", { src: url, alt: a.note }));
    } else if (ext === "mp4") {
      art.append(el("video", { src: url, controls: true }));
    } else if (ext === "txt") {
      art.append(el("pre", { textContent: new TextDecoder().decode(a.data) }));
    }
    art.append(el("a", { href: url, download: a.name, textContent: "Download " + a.name }));
    box.append(art);
  }
  node.append(el("h4", { className: "revealed-title", textContent: "What the file was hiding" }), box);
}

function seal(r, note) {
  const out = sphinx.seal(r.bytes);
  if (!(out instanceof Uint8Array)) {
    note.textContent = "Could not clean this file: " + out.error;
    return;
  }
  const left = JSON.parse(sphinx.inspect(out)).findings.filter((f) => f.severity !== "low");
  const dot = r.name.lastIndexOf(".");
  const name = dot > 0 ? r.name.slice(0, dot) + ".sealed" + r.name.slice(dot) : r.name + ".sealed";
  download(name, out, "application/octet-stream");
  note.textContent = left.length === 0
    ? "Saved as " + name + "."
    : "Saved as " + name + ". These need a human decision and remain: " + [...new Set(left.map((f) => f.title.toLowerCase()))].join("; ") + ".";
}

function exportReport(kind) {
  const files = results.map((r) => r.error
    ? { path: r.name, error: r.error, findings: [] }
    : { path: r.name, format: r.report.format, verdict: r.report.verdict, findings: r.report.findings.map(({ why, fix, title, ...f }) => f) });
  if (kind === "json") {
    download("sphinx-report.json", JSON.stringify({ tool: "sphinx", files }, null, 2), "application/json");
    return;
  }
  const cell = (s) => String(s || "").replace(/\|/g, "\\|").replace(/\n/g, " ");
  let md = "### Sphinx report\n\n";
  for (const f of files) {
    if (f.error) { md += `**\`${f.path}\`** could not be read: ${cell(f.error)}\n\n`; continue; }
    if (!f.findings.length) continue;
    md += `**\`${f.path}\`** ${formatName[f.format] || f.format}: ${f.verdict}\n\n| Severity | Rule | What it gives away | Evidence |\n|---|---|---|---|\n`;
    for (const x of f.findings) md += `| ${x.severity} | \`${x.rule}\` | ${cell(x.message)} | ${cell(x.evidence)} |\n`;
    md += "\n";
  }
  download("sphinx-report.md", md, "text/markdown");
}

function renderRules(group) {
  const rules = JSON.parse(sphinx.rules());
  $("questions-lede").textContent = `${rules.length} questions, from the ones that end careers to the ones that only whisper. Each comes with why it matters and what to do about it.`;
  const bar = $("filters");
  bar.replaceChildren();
  for (const name of Object.keys(groups)) {
    const b = el("button", { type: "button", textContent: name, className: name === group ? "active" : "" });
    b.setAttribute("aria-pressed", name === group);
    b.addEventListener("click", () => renderRules(name));
    bar.append(b);
  }
  const box = $("rules");
  box.replaceChildren();
  const want = groups[group];
  for (const r of rules) {
    if (want && !r.Formats.some((f) => want.includes(f))) continue;
    box.append(el("div", { className: "rule" },
      el("div", { className: "meta" }, el("span", { textContent: r.ID }), el("span", { className: "sev sev-" + r.Severity, textContent: r.Severity })),
      el("h3", { textContent: r.Title }),
      el("p", { textContent: r.Why }),
      el("p", { className: "formats", textContent: r.Formats.map((f) => f.toUpperCase()).join(" · ") })));
  }
}

function fromFiles(list) {
  return [...list].map((f) => ({ name: f.webkitRelativePath || f.name, size: f.size, load: async () => new Uint8Array(await f.arrayBuffer()) }));
}

async function fromDrop(dt) {
  const entries = [...dt.items].map((i) => i.webkitGetAsEntry && i.webkitGetAsEntry()).filter(Boolean);
  if (!entries.length) return fromFiles(dt.files);
  const out = [];
  const walk = async (entry, prefix) => {
    if (entry.isFile) {
      const file = await new Promise((res, rej) => entry.file(res, rej));
      out.push({ name: prefix + file.name, size: file.size, load: async () => new Uint8Array(await file.arrayBuffer()) });
      return;
    }
    const reader = entry.createReader();
    for (;;) {
      const batch = await new Promise((res, rej) => reader.readEntries(res, rej));
      if (!batch.length) break;
      for (const e of batch) if (!e.name.startsWith(".") || e.name === ".DS_Store") await walk(e, prefix + entry.name + "/");
    }
  };
  for (const e of entries) await walk(e, "");
  return out;
}

const drop = $("drop");
$("pick-files").addEventListener("click", (e) => { e.stopPropagation(); $("files").click(); });
$("pick-folder").addEventListener("click", (e) => { e.stopPropagation(); $("folder").click(); });
drop.addEventListener("click", () => $("files").click());
drop.addEventListener("keydown", (e) => {
  if (e.key === "Enter" || e.key === " ") { e.preventDefault(); $("files").click(); }
});
for (const id of ["files", "folder"]) {
  $(id).addEventListener("change", (e) => {
    if (e.target.files.length) examine(fromFiles(e.target.files));
    e.target.value = "";
  });
}
for (const ev of ["dragenter", "dragover"]) drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.add("over"); });
for (const ev of ["dragleave", "drop"]) drop.addEventListener(ev, () => drop.classList.remove("over"));
drop.addEventListener("drop", async (e) => {
  e.preventDefault();
  examine(await fromDrop(e.dataTransfer));
});
for (const b of document.querySelectorAll("[data-example]")) {
  b.addEventListener("click", async () => {
    const name = b.dataset.example;
    examine([{ name, size: 0, load: async () => new Uint8Array(await (await fetch("examples/" + name)).arrayBuffer()) }]);
  });
}
$("export-json").addEventListener("click", () => exportReport("json"));
$("export-md").addEventListener("click", () => exportReport("md"));
$("clear").addEventListener("click", () => {
  results.length = 0;
  renderResults();
  status("");
});
