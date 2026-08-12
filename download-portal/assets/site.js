"use strict";

const getNode = (id) => document.getElementById(id);

const setText = (id, value) => {
  const node = getNode(id);
  if (node) node.textContent = value;
};

const enableReleaseLink = (id, value) => {
  const node = getNode(id);
  if (!node) return;
  node.setAttribute("href", value);
  node.removeAttribute("aria-disabled");
  node.removeAttribute("tabindex");
};

const formatPublishedAt = (value) => {
  const publishedAt = new Date(value);
  if (Number.isNaN(publishedAt.getTime())) return value;
  return new Intl.DateTimeFormat("tr-TR", {
    dateStyle: "long",
    timeStyle: "short",
    timeZone: "Europe/Istanbul",
  }).format(publishedAt);
};

const setReleaseReady = () => {
  const panel = document.querySelector(".release-panel");
  if (panel) panel.setAttribute("aria-busy", "false");
  setText("release-status", "İndirmeye hazır");

  const exactCopy = getNode("exact-copy");
  if (exactCopy) exactCopy.disabled = false;
};

const setReleaseError = (error) => {
  const panel = document.querySelector(".release-panel");
  if (panel) {
    panel.setAttribute("aria-busy", "false");
    panel.classList.add("release-error");
  }
  setText("release-version", "Sürüm bilgisi alınamadı");
  setText("release-status", "Manifest şu anda kullanılamıyor");
  setText("release-date", error.message);
  setText("exact-command", "Sabit sürüm komutu için manifest bağlantısını kontrol edin.");
};

fetch("/releases/latest.json", { cache: "no-store", credentials: "omit" })
  .then((response) => {
    if (!response.ok) throw new Error("HTTP " + response.status);
    return response.json();
  })
  .then((release) => {
    setText("release-version", release.version);
    setText("release-date", formatPublishedAt(release.published_at));
    setText("release-commit", release.commit);
    setText("release-sha", release.sha256);
    enableReleaseLink("archive-link", release.archive_url);
    enableReleaseLink("checksum-link", release.checksum_url);
    setText(
      "exact-command",
      "curl --fail --show-error --location --proto '=https' --tlsv1.2 https://celikpanel.net/get.sh -o /tmp/celikpanel-get.sh\n" +
        "sh /tmp/celikpanel-get.sh --version " + release.version,
    );
    setReleaseReady();
  })
  .catch(setReleaseError);

document.addEventListener("click", async (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;

  const button = target.closest("[data-copy]");
  if (!(button instanceof HTMLButtonElement) || button.disabled) return;

  const source = getNode(button.dataset.copy);
  if (!source) return;

  const label = button.querySelector("[data-copy-label]");
  const status = getNode("copy-status");
  const resetLabel = () => {
    if (label) label.textContent = "Kopyala";
    button.classList.remove("copied");
  };

  try {
    await navigator.clipboard.writeText(source.textContent || "");
    if (label) label.textContent = "Kopyalandı";
    if (status) status.textContent = "Kurulum komutu panoya kopyalandı.";
    button.classList.add("copied");
  } catch {
    if (label) label.textContent = "Metni seçin";
    if (status) status.textContent = "Pano kullanılamadı; komutu seçerek kopyalayın.";
  }

  window.setTimeout(resetLabel, 1800);
});
