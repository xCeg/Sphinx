"use strict";

const $ = (id) => document.getElementById(id);
const sealable = new Set(["png", "jpeg", "docx", "xlsx", "pptx"]);
const verdictText = {
  betrays: "It betrays you.",
  whispers: "It whispers a little.",
  silent: "It keeps silent.",
};
const mime = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", mp4: "video/mp4", txt: "text/plain", pdf: "application/pdf" };

let current = null;

const ready = (async () => {
  const go = new Go();
  const res = await fetch("sphinx.wasm");
  const { instance } = await WebAssembly.instantiateStreaming(res, go.importObject);
  go.run(instance);
  renderRules();
})();

function el(tag, props = {}, ...children) {
  const node = Object.assign(document.createElement(tag), props);
  for (const c of children) if (c != null) node.append(c);
  return node;
}

function status(text) {
  $("status").textContent = text;
}

async function examine(name, bytes) {
  status("The Sphinx is looking at " + name + "…");
  try {
    await ready;
  } catch (e) {
    status("The inspector failed to load: " + e.message);
    return;
  }
  const raw = sphinx.inspect(bytes);
  if (typeof raw !== "string") {
    status(name + ": " + raw.error);
    $("result").hidden = true;
    return;
  }
  const report = JSON.parse(raw);
  current = { name, bytes, report };
  status("");
  render(report, name);
  reveal(bytes, report.format);
  $("result").scrollIntoView({ behavior: "smooth", block: "start" });
}

function render(report, name) {
  $("result").hidden = false;
  $("result-file").textContent = name;
  const v = $("result-verdict");
  v.textContent = verdictText[report.verdict] || report.verdict;
  v.className = "result-verdict verdict-" + report.verdict;
  $("seal").hidden = !sealable.has(report.format) || report.findings.length === 0;

  const list = $("findings");
  list.replaceChildren();
  if (report.findings.length === 0) {
    list.append(el("li", { className: "finding" }, el("span", { className: "sev sev-low", textContent: "none" }),
      el("div", {}, el("p", { textContent: "Nothing found. That does not prove the file is safe, only that none of the eighteen questions found an answer." }))));
  }
  for (const f of report.findings) {
    const body = el("div", {},
      el("h3", { textContent: f.title }),
      el("p", { textContent: f.message }),
      f.evidence ? el("p", { className: "evidence", textContent: f.evidence }) : null,
      el("details", {},
        el("summary", { textContent: "Why it matters and what to do" }),
        el("p", { textContent: f.why }),
        el("p", { textContent: f.fix }),
        el("p", { className: "rule-id", textContent: f.rule + (f.location ? " · " + f.location : "") })));
    list.append(el("li", { className: "finding" }, el("span", { className: "sev sev-" + f.severity, textContent: f.severity }), body));
  }
}

function reveal(bytes, format) {
  const arts = sphinx.reveal(bytes);
  const box = $("artifacts");
  box.replaceChildren();
  if (!Array.isArray(arts) || arts.length === 0) {
    $("revealed").hidden = true;
    return;
  }
  $("revealed").hidden = false;
  if (format === "png" || format === "jpeg") {
    const url = URL.createObjectURL(new Blob([bytes], { type: "image/" + format }));
    box.append(el("div", { className: "artifact seen" }, el("p", { textContent: "What anyone opening the file sees" }), el("img", { src: url, alt: "the visible picture" })));
  }
  for (const a of arts) {
    const ext = a.name.split(".").pop().toLowerCase();
    const blob = new Blob([a.data], { type: mime[ext] || "application/octet-stream" });
    const url = URL.createObjectURL(blob);
    const card = el("div", { className: "artifact" }, el("p", { textContent: a.note }));
    if (ext === "png" || ext === "jpg" || ext === "jpeg") {
      card.append(el("img", { src: url, alt: a.note }));
    } else if (ext === "mp4") {
      card.append(el("video", { src: url, controls: true }));
    } else if (ext === "txt") {
      card.append(el("pre", { textContent: new TextDecoder().decode(a.data) }));
    }
    card.append(el("a", { href: url, download: a.name, textContent: "Download " + a.name }));
    box.append(card);
  }
}

function seal() {
  if (!current) return;
  const out = sphinx.seal(current.bytes);
  if (!(out instanceof Uint8Array)) {
    status("Could not clean this file: " + out.error);
    return;
  }
  const left = JSON.parse(sphinx.inspect(out)).findings.filter((f) => f.severity !== "low");
  const dot = current.name.lastIndexOf(".");
  const name = dot > 0 ? current.name.slice(0, dot) + ".sealed" + current.name.slice(dot) : current.name + ".sealed";
  const a = el("a", { href: URL.createObjectURL(new Blob([out])), download: name });
  document.body.append(a);
  a.click();
  a.remove();
  status(left.length === 0
    ? "Cleaned copy saved as " + name + "."
    : "Cleaned copy saved as " + name + ". " + left.length + " issue(s) need a human decision and remain: " + left.map((f) => f.title.toLowerCase()).join("; ") + ".");
}

function renderRules() {
  const box = $("rules");
  for (const r of JSON.parse(sphinx.rules())) {
    box.append(el("div", { className: "rule" },
      el("div", { className: "meta" }, el("span", { textContent: r.ID }), el("span", { className: "sev sev-" + r.Severity, textContent: r.Severity })),
      el("h3", { textContent: r.Title }),
      el("p", { textContent: r.Why })));
  }
}

async function readFile(file) {
  examine(file.name, new Uint8Array(await file.arrayBuffer()));
}

const drop = $("drop");
$("file").addEventListener("change", (e) => e.target.files[0] && readFile(e.target.files[0]));
drop.addEventListener("keydown", (e) => (e.key === "Enter" || e.key === " ") && $("file").click());
for (const ev of ["dragenter", "dragover"]) drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.add("over"); });
for (const ev of ["dragleave", "drop"]) drop.addEventListener(ev, () => drop.classList.remove("over"));
drop.addEventListener("drop", (e) => {
  e.preventDefault();
  if (e.dataTransfer.files[0]) readFile(e.dataTransfer.files[0]);
});
for (const b of document.querySelectorAll("[data-example]")) {
  b.addEventListener("click", async () => {
    const name = b.dataset.example;
    const res = await fetch("examples/" + name);
    examine(name, new Uint8Array(await res.arrayBuffer()));
  });
}
$("seal").addEventListener("click", seal);
