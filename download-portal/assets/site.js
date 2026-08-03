"use strict";

const text = (id, value) => {
  const node = document.getElementById(id);
  if (node) node.textContent = value;
};

const link = (id, value) => {
  const node = document.getElementById(id);
  if (node) node.setAttribute("href", value);
};

fetch("/releases/latest.json", { cache: "no-store", credentials: "omit" })
  .then((response) => {
    if (!response.ok) throw new Error("release metadata returned " + response.status);
    return response.json();
  })
  .then((release) => {
    text("release-version", release.version);
    text("release-date", new Date(release.published_at).toLocaleString());
    text("release-commit", release.commit);
    text("release-sha", release.sha256);
    link("archive-link", release.archive_url);
    link("checksum-link", release.checksum_url);
    text(
      "exact-command",
      "curl --fail --show-error --location --proto '=https' --tlsv1.2 https://celikpanel.net/get.sh -o /tmp/celikpanel-get.sh\n" +
        "sh /tmp/celikpanel-get.sh --version " + release.version,
    );
  })
  .catch((error) => {
    text("release-version", "Metadata unavailable");
    text("release-date", error.message);
  });

document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-copy]");
  if (!button) return;
  const source = document.getElementById(button.dataset.copy);
  if (!source) return;
  try {
    await navigator.clipboard.writeText(source.textContent);
    button.textContent = "Copied";
    window.setTimeout(() => { button.textContent = "Copy"; }, 1400);
  } catch {
    button.textContent = "Select and copy";
  }
});
