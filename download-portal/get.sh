#!/bin/sh
set -eu
umask 077

base_url=https://celikpanel.net
requested_version=latest
requested_action=auto
releases_root=/var/backups/celikpanel/releases
workdir=

message() {
  printf '%s / %s\n' "$1" "$2"
}

fail() {
  message "$1" "$2" >&2
  exit 1
}

usage() {
  message \
    "Usage: $0 [--version vX.Y.Z[-prerelease]] [--install|--update]" \
    "Kullanım: $0 [--version vX.Y.Z[-önsürüm]] [--install|--update]"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { usage >&2; exit 2; }
      requested_version=$2
      shift 2
      ;;
    --install|--update)
      [ "$requested_action" = auto ] || {
        message "Choose only one operation mode." "Yalnız bir işlem modu seçin." >&2
        exit 2
      }
      requested_action=${1#--}
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

[ "$(id -u)" -eq 0 ] || fail \
  "CelikPanel installation and updates must run as root." \
  "CelikPanel kurulumu ve güncellemeleri root olarak çalıştırılmalıdır."

for required_command in awk bash chmod chown cmp curl dirname env find grep id install \
  mkdir mktemp mv od readlink rm sha256sum sort stat sync tar tr xargs; do
  command -v "$required_command" >/dev/null 2>&1 || fail \
    "$required_command is required. Install it with your operating system package manager." \
    "$required_command gereklidir. İşletim sisteminizin paket yöneticisiyle kurun."
done

curl_fetch() {
  curl --fail --show-error --silent --location \
    --proto '=https' --tlsv1.2 --connect-timeout 20 --retry 3 \
    "$1" -o "$2"
}

cleanup() {
  [ -n "$workdir" ] || return 0
  case "$workdir" in
    /tmp/celikpanel-install.*|"$releases_root"/.download.*)
      rm -rf -- "$workdir"
      ;;
    *)
      message \
        "Refusing to clean an unexpected work directory: $workdir" \
        "Beklenmeyen çalışma dizini temizlenmiyor: $workdir" >&2
      ;;
  esac
}
trap cleanup EXIT HUP INT TERM

validate_root_directory_chain() {
  path=$1
  canonical=$(readlink -e -- "$path") || fail \
    "Trusted directory is unavailable: $path" \
    "Güvenilir dizin kullanılamıyor: $path"
  [ "$canonical" = "$path" ] || fail \
    "Trusted directory contains a symlink or alias: $path" \
    "Güvenilir dizin sembolik bağlantı veya takma yol içeriyor: $path"
  current=$path
  while :; do
    [ -d "$current" ] && [ ! -L "$current" ] || fail \
      "Unsafe trusted directory: $current" \
      "Güvenilir dizin güvenli değil: $current"
    set -- $(stat -Lc '%u %g %a' -- "$current")
    owner=$1
    group=$2
    mode=$3
    [ "$owner" -eq 0 ] && [ "$group" -eq 0 ] || fail \
      "Trusted directory must be owned by root: $current" \
      "Güvenilir dizin root sahipli olmalı: $current"
    permissions=$((0$mode))
    [ $((permissions & 0022)) -eq 0 ] || fail \
      "Trusted directory must not be group/other writable: $current" \
      "Güvenilir dizin grup/diğer kullanıcılarca yazılabilir olmamalı: $current"
    [ "$current" = / ] && break
    current=$(dirname -- "$current")
  done
}

prepare_release_storage() {
  validate_root_directory_chain /var
  for directory in /var/backups /var/backups/celikpanel "$releases_root"; do
    if [ ! -e "$directory" ] && [ ! -L "$directory" ]; then
      desired_mode=0700
      [ "$directory" != /var/backups ] || desired_mode=0755
      install -d -m "$desired_mode" -o root -g root -- "$directory"
      sync -f -- "$(dirname -- "$directory")"
    fi
    validate_root_directory_chain "$directory"
  done
  set -- $(stat -Lc '%u %g %a' -- "$releases_root")
  [ "$1:$2:$3" = 0:0:700 ] || fail \
    "Release storage must be root:root mode 0700." \
    "Sürüm deposu root:root 0700 kipinde olmalı."
}

marker_state=absent
if [ -e /etc/celikpanel/install.complete ] || [ -L /etc/celikpanel/install.complete ]; then
  marker_state=invalid
  if [ -f /etc/celikpanel/install.complete ] && [ ! -L /etc/celikpanel/install.complete ]; then
    set -- $(stat -Lc '%u %g %a %h' -- /etc/celikpanel/install.complete)
    [ "$1:$2:$3:$4" = 0:0:600:1 ] && marker_state=valid
  fi
fi

full_install=1
for installed_path in \
  /opt/celikpanel/bin/panel \
  /opt/celikpanel/bin/agent \
  /etc/systemd/system/celikpanel-panel.service \
  /etc/systemd/system/celikpanel-agent.service \
  /etc/celikpanel/panel.env \
  /var/lib/celikpanel/celikpanel.db; do
  if [ ! -f "$installed_path" ] || [ -L "$installed_path" ]; then
    full_install=0
  fi
done

any_install=0
for installed_path in \
  /etc/celikpanel/install.complete \
  /opt/celikpanel/bin/panel \
  /opt/celikpanel/bin/agent \
  /etc/systemd/system/celikpanel-panel.service \
  /etc/systemd/system/celikpanel-agent.service \
  /etc/celikpanel/panel.env \
  /var/lib/celikpanel/celikpanel.db; do
  if [ -e "$installed_path" ] || [ -L "$installed_path" ]; then
    any_install=1
  fi
done

panel_active=0
agent_active=0
if command -v systemctl >/dev/null 2>&1; then
  systemctl is-active --quiet celikpanel-panel.service 2>/dev/null && panel_active=1 || true
  systemctl is-active --quiet celikpanel-agent.service 2>/dev/null && agent_active=1 || true
fi

operation=$requested_action
if [ "$operation" = auto ]; then
  if [ "$full_install" -eq 1 ] && { [ "$marker_state" = valid ] || { [ "$panel_active" -eq 1 ] && [ "$agent_active" -eq 1 ]; }; }; then
    operation=update
  elif [ "$any_install" -eq 0 ]; then
    operation=install
  else
    fail \
      "A partial or ambiguous CelikPanel installation was found. Retry with --install after a failed first setup, or use --update only after verifying the existing installation." \
      "Yarım veya belirsiz bir CelikPanel kurulumu bulundu. İlk kurulum başarısız olduysa --install ile yeniden deneyin; --update seçeneğini yalnız mevcut kurulumu doğruladıktan sonra kullanın."
  fi
fi

if [ "$operation" = install ]; then
  [ "$marker_state" = absent ] && [ "$panel_active" -eq 0 ] || fail \
    "A completed or running CelikPanel installation already exists; use --update." \
    "Tamamlanmış veya çalışan bir CelikPanel kurulumu zaten var; --update kullanın."
  workdir=$(mktemp -d /tmp/celikpanel-install.XXXXXXXX)
  chmod 0700 "$workdir"
else
  [ "$full_install" -eq 1 ] || fail \
    "The installed CelikPanel layout is incomplete; refusing an update." \
    "Kurulu CelikPanel yerleşimi eksik; güncelleme reddedildi."
  [ "$marker_state" != invalid ] || fail \
    "The installation-complete marker is unsafe; refusing an update." \
    "Kurulum-tamamlandı işareti güvenli değil; güncelleme reddedildi."
  prepare_release_storage
  workdir=$(mktemp -d "$releases_root/.download.XXXXXXXX")
  chmod 0700 "$workdir"
  chown root:root "$workdir"
fi

if [ "$requested_version" = latest ]; then
  curl_fetch "$base_url/releases/latest.txt" "$workdir/latest.txt"
  version=$(tr -d '\r\n\t ' < "$workdir/latest.txt")
else
  version=$requested_version
fi

printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' || fail \
  "Unsafe or invalid release version: $version" \
  "Güvensiz veya geçersiz sürüm: $version"

archive=celikpanel-$version.tar.gz
release_url=$base_url/releases/$version
curl_fetch "$release_url/$archive" "$workdir/$archive"
curl_fetch "$release_url/$archive.sha256" "$workdir/$archive.sha256"

expected_line=$(tr -d '\r' < "$workdir/$archive.sha256")
set -- $expected_line
[ "$#" -eq 2 ] || fail \
  "Checksum file has an unexpected format." \
  "Sağlama toplamı dosyası beklenmeyen biçimde."
checksum_value=$1
checksum_name=$2
[ "$checksum_name" = "$archive" ] && [ "${#checksum_value}" -eq 64 ] || fail \
  "Checksum file has an unexpected format." \
  "Sağlama toplamı dosyası beklenmeyen biçimde."
case "$checksum_value" in
  *[!0-9a-fA-F]*) fail \
    "Checksum file has an unexpected format." \
    "Sağlama toplamı dosyası beklenmeyen biçimde." ;;
esac
(cd "$workdir" && sha256sum -c "$archive.sha256")

root=celikpanel-$version
tar -tzf "$workdir/$archive" | awk -v root="$root" '
  BEGIN { ok = 1; count = 0 }
  {
    count++
    if ($0 ~ /^\// || $0 ~ /\\/ || $0 == ".." || $0 ~ /^\.\.\// || $0 ~ /\/\.\.($|\/)/) ok = 0
    if ($0 != root "/" && index($0, root "/") != 1) ok = 0
  }
  END { if (!ok || count == 0) exit 1 }
' || fail \
  "Archive contains unsafe or unexpected paths." \
  "Arşiv güvensiz veya beklenmeyen yollar içeriyor."

tar -tvzf "$workdir/$archive" | awk '
  { type = substr($0, 1, 1); if (type != "-" && type != "d") exit 1 }
' || fail \
  "Archive contains links or special filesystem objects." \
  "Arşiv bağlantı veya özel dosya sistemi nesneleri içeriyor."

mkdir "$workdir/extract"
tar -xzf "$workdir/$archive" -C "$workdir/extract" \
  --no-same-owner --no-same-permissions
extracted_root=$workdir/extract/$root
[ -d "$extracted_root" ] && [ ! -L "$extracted_root" ] || fail \
  "The verified archive does not contain the expected release root." \
  "Doğrulanan arşiv beklenen sürüm kökünü içermiyor."
if find "$extracted_root" -xdev -type l -print -quit | grep -q .; then
  fail "The extracted release contains a symbolic link." "Çıkarılan sürüm sembolik bağlantı içeriyor."
fi
if find "$extracted_root" -xdev ! -type d ! -type f -print -quit | grep -q .; then
  fail "The extracted release contains a special filesystem object." "Çıkarılan sürüm özel dosya sistemi nesnesi içeriyor."
fi
if find "$extracted_root" -xdev -type f -links +1 -print -quit | grep -q .; then
  fail "The extracted release contains a hard-linked file." "Çıkarılan sürüm hard-link dosyası içeriyor."
fi

manifest=$extracted_root/SHA256SUMS
[ -f "$manifest" ] && [ ! -L "$manifest" ] || fail \
  "The extracted release does not contain a regular SHA256SUMS manifest." \
  "Çıkarılan sürüm normal bir SHA256SUMS manifesti içermiyor."
(
  cd "$extracted_root"
  LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
    | LC_ALL=C sort -z \
    | xargs -0 sha256sum \
    | cmp -s - SHA256SUMS
  sha256sum -c SHA256SUMS >/dev/null
) || fail \
  "The extracted release does not match its exact internal checksum manifest." \
  "Çıkarılan sürüm birebir iç sağlama toplamı manifestiyle eşleşmiyor."

for metadata_name in release.version release.commit release.tree; do
  metadata_path=$extracted_root/$metadata_name
  [ -f "$metadata_path" ] && [ ! -L "$metadata_path" ] || fail \
    "The extracted release is missing verified provenance metadata." \
    "Çıkarılan sürümde doğrulanmış köken bilgisi eksik."
done
[ "$(tr -d '\r\n\t ' < "$extracted_root/release.version")" = 1 ] || fail \
  "The extracted release format is unsupported." \
  "Çıkarılan sürüm biçimi desteklenmiyor."
for metadata_name in release.commit release.tree; do
  metadata_value=$(tr -d '\r\n\t ' < "$extracted_root/$metadata_name")
  printf '%s\n' "$metadata_value" | grep -Eq '^[0-9a-f]{40,64}$' || fail \
    "The extracted release contains invalid provenance metadata." \
    "Çıkarılan sürüm geçersiz köken bilgisi içeriyor."
done

if [ "$operation" = install ]; then
  installer=$extracted_root/install.sh
  [ -f "$installer" ] && [ ! -L "$installer" ] || fail \
    "The verified archive does not contain a regular install.sh." \
    "Doğrulanan arşiv normal bir install.sh dosyası içermiyor."
  message \
    "Installing CelikPanel $version from verified archive $archive" \
    "CelikPanel $version doğrulanmış $archive arşivinden kuruluyor"
  cd "$extracted_root"
  bash "$installer"
else
  updater=$extracted_root/bootstrap-prebuilt-update.sh
  [ -f "$updater" ] && [ ! -L "$updater" ] || fail \
    "The verified archive does not contain the prebuilt update entry point." \
    "Doğrulanan arşiv hazır güncelleme giriş noktasını içermiyor."
  message \
    "Updating CelikPanel to $version from verified prebuilt archive $archive" \
    "CelikPanel doğrulanmış hazır $archive arşivinden $version sürümüne güncelleniyor"
  bash "$updater" --normal
  install -m 0600 -o root -g root /dev/null /etc/celikpanel/install.complete
  sync -f /etc/celikpanel/install.complete /etc/celikpanel
fi
