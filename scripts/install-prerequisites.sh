#!/usr/bin/env bash
# Install controller-side tools needed by one or more repository deployment targets.
set -euo pipefail

dry_run=false
want_docker=false
want_aws=false
want_gcp=false
want_azure=false
want_libvirt=false
want_ui=false
all_targets=false
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)

usage() {
  cat <<'EOF'
Usage: scripts/install-prerequisites.sh [options]

Installs common controller tools (Git, Terraform 1.9+, Ansible core 2.14+,
OpenSSH, curl, unzip, Python 3.9+) and pinned Ansible collections by default.
Select deployment-specific prerequisites with:

  --docker       Docker Desktop (macOS) or Docker Engine (Linux)
  --aws          AWS CLI v2
  --gcp          Google Cloud CLI
  --azure        Azure CLI
  --libvirt      KVM/Libvirt and genisoimage (Linux only)
  --ui           Go 1.22+ for ui-go
  --all          All applicable target-specific prerequisites
  --dry-run      Print commands without executing them
  -h, --help     Show this help

Supported hosts: macOS with an existing Homebrew installation, Debian-family
Linux, and RHEL-family Linux. This script does not configure cloud credentials,
generate SSH keys, start Docker Desktop, or install the private CHAOS provider.
EOF
}

die() { printf 'error: %s\n' "$*" >&2; exit 1; }
note() { printf '%s\n' "$*"; }

run() {
  if "$dry_run"; then
    printf '+ '
    printf '%q ' "$@"
    printf '\n'
  else
    "$@"
  fi
}

run_shell() {
  if "$dry_run"; then
    printf '+ %s\n' "$1"
  else
    bash -c "$1"
  fi
}

need_sudo() {
  "$dry_run" || sudo -v
}

while (($#)); do
  case "$1" in
    --docker) want_docker=true ;;
    --aws) want_aws=true ;;
    --gcp) want_gcp=true ;;
    --azure) want_azure=true ;;
    --libvirt) want_libvirt=true ;;
    --ui) want_ui=true ;;
    --all) all_targets=true; want_docker=true; want_aws=true; want_gcp=true; want_azure=true; want_libvirt=true; want_ui=true ;;
    --dry-run) dry_run=true ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (use --help)" ;;
  esac
  shift
done

os_name=$(uname -s)
platform=""
if [[ "$os_name" == "Darwin" ]]; then
  platform="macos"
elif [[ "$os_name" == "Linux" && -r /etc/os-release ]]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  case " ${ID_LIKE:-} ${ID:-} " in
    *" debian "*|*" ubuntu "*) platform="debian" ;;
    *" rhel "*|*" fedora "*|*" centos "*|*" rocky "*|*" almalinux "*) platform="rhel" ;;
    *) die "unsupported Linux distribution: ${PRETTY_NAME:-$ID}" ;;
  esac
else
  die "unsupported operating system: $os_name"
fi

if [[ "$platform" == "rhel" ]]; then
  if command -v dnf >/dev/null 2>&1; then
    pkg_manager=dnf
    pkg_plugins=dnf-plugins-core
  elif command -v yum >/dev/null 2>&1; then
    pkg_manager=yum
    pkg_plugins=yum-utils
  else
    die "neither dnf nor yum is available"
  fi
  rhel_major=${VERSION_ID%%.*}
fi

if [[ "$platform" == "macos" && "$want_libvirt" == true ]]; then
  if "$all_targets"; then
    want_libvirt=false
    note "Skipping Libvirt: KVM is not supported on macOS."
  else
    die "--libvirt is supported only on Linux hosts with KVM"
  fi
fi

if [[ "$platform" == "macos" && "$dry_run" == false ]] && ! command -v brew >/dev/null 2>&1; then
  die "Homebrew is required on macOS. Install it from https://brew.sh/ and run this script again."
fi

apt_update_done=false
apt_update() {
  if ! "$apt_update_done"; then
    need_sudo
    run sudo apt-get update
    apt_update_done=true
  fi
}

apt_install() { apt_update; run sudo apt-get install -y "$@"; }
dnf_install() { need_sudo; run sudo "${pkg_manager}" install -y "$@"; }

add_hashicorp_repo_debian() {
  apt_install ca-certificates curl gnupg lsb-release
  run_shell 'curl -fsSL https://apt.releases.hashicorp.com/gpg | sudo gpg --yes --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg'
  run_shell 'printf "%s\\n" "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(. /etc/os-release && printf %s "$VERSION_CODENAME") main" | sudo tee /etc/apt/sources.list.d/hashicorp.list >/dev/null'
  apt_update_done=false
}

add_hashicorp_repo_rhel() {
  need_sudo
  run sudo "${pkg_manager}" install -y "${pkg_plugins}"
  run sudo "${pkg_manager}" config-manager --add-repo https://rpm.releases.hashicorp.com/RHEL/hashicorp.repo
}

install_common() {
  case "$platform" in
    macos)
      run brew tap hashicorp/tap
      run brew install hashicorp/tap/terraform
      run brew install git ansible curl unzip openssh python openssl@3
      ;;
    debian)
      add_hashicorp_repo_debian
      apt_install git terraform ansible openssh-client curl unzip python3 python3-pip openssl
      ;;
    rhel)
      dnf_install git curl unzip openssh-clients python3 python3-pip ansible-core openssl
      add_hashicorp_repo_rhel
      dnf_install terraform
      ;;
  esac
}

# Fail with useful guidance when an OS repository supplies an obsolete tool.
# Dry runs do not execute or require any of the tools being installed.
check_minimum_version() {
  local name=$1 minimum=$2 output=$3
  if ! python3 -c 'import re, sys
match = re.search(r"(\d+)\.(\d+)(?:\.(\d+))?", sys.argv[3])
version = tuple(int(x or 0) for x in match.groups()) if match else ()
minimum = tuple(int(x) for x in sys.argv[2].split("."))
sys.exit(0 if version and version >= minimum else 1)' "$name" "$minimum" "$output"; then
    die "${name} ${minimum}+ is required; the installed version is missing or too old. Install a supported version and rerun this script."
  fi
}

install_ansible_collections() {
  if ! "$dry_run"; then
    check_minimum_version Python 3.9 "$(python3 --version 2>&1)"
    check_minimum_version Terraform 1.9 "$(terraform version 2>&1)"
    check_minimum_version Ansible 2.14 "$(ansible --version 2>&1)"
  fi
  run ansible-galaxy collection install --requirements-file "${repo_root}/ansible/requirements.yml"
}

install_docker() {
  case "$platform" in
    macos) run brew install --cask docker ;;
    debian)
      apt_install ca-certificates curl gnupg
      run_shell 'sudo install -m 0755 -d /etc/apt/keyrings && curl -fsSL https://download.docker.com/linux/'"${ID}"'/gpg | sudo gpg --yes --dearmor -o /etc/apt/keyrings/docker.gpg && sudo chmod a+r /etc/apt/keyrings/docker.gpg'
      run_shell 'printf "%s\\n" "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/'"${ID}"' $(. /etc/os-release && printf %s "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null'
      apt_update_done=false
      apt_install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
      run sudo systemctl enable --now docker
      ;;
    rhel)
      need_sudo
      run sudo "${pkg_manager}" install -y "${pkg_plugins}"
      run sudo "${pkg_manager}" config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
      dnf_install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
      run sudo systemctl enable --now docker
      ;;
  esac
}

install_aws() {
  case "$platform" in
    macos) run brew install awscli ;;
    debian|rhel)
      local arch
      case "$(uname -m)" in x86_64) arch=x86_64 ;; aarch64|arm64) arch=aarch64 ;; *) die "AWS CLI v2 is unsupported on $(uname -m)" ;; esac
      run curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${arch}.zip" -o /tmp/awscliv2.zip
      run unzip -oq /tmp/awscliv2.zip -d /tmp
      need_sudo
      run sudo /tmp/aws/install --update
      run rm -rf /tmp/aws /tmp/awscliv2.zip
      ;;
  esac
}

install_gcp() {
  case "$platform" in
    macos) run brew install --cask google-cloud-sdk ;;
    debian)
      apt_install ca-certificates curl gnupg
      run_shell 'curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo gpg --yes --dearmor -o /usr/share/keyrings/cloud.google.gpg'
      run_shell 'printf "%s\\n" "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | sudo tee /etc/apt/sources.list.d/google-cloud-sdk.list >/dev/null'
      apt_update_done=false
      apt_install google-cloud-cli
      ;;
    rhel)
      [[ "$(uname -m)" == "x86_64" ]] || die "Google Cloud CLI RPM packages are only supported on x86_64 RHEL-family hosts"
      need_sudo
      run_shell 'printf "%s\\n" "[google-cloud-cli]" "name=Google Cloud CLI" "baseurl=https://packages.cloud.google.com/yum/repos/cloud-sdk-el8-x86_64" "enabled=1" "gpgcheck=1" "repo_gpgcheck=0" "gpgkey=https://packages.cloud.google.com/yum/doc/rpm-package-key.gpg" | sudo tee /etc/yum.repos.d/google-cloud-sdk.repo >/dev/null'
      dnf_install google-cloud-cli
      ;;
  esac
}

install_azure() {
  case "$platform" in
    macos) run brew install azure-cli ;;
    debian)
      apt_install ca-certificates curl gnupg lsb-release
      run_shell 'curl -fsSL https://packages.microsoft.com/keys/microsoft.asc | sudo gpg --yes --dearmor -o /usr/share/keyrings/microsoft-prod.gpg'
      run_shell 'printf "%s\\n" "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/microsoft-prod.gpg] https://packages.microsoft.com/repos/azure-cli/ $(. /etc/os-release && printf %s "$VERSION_CODENAME") main" | sudo tee /etc/apt/sources.list.d/azure-cli.list >/dev/null'
      apt_update_done=false
      apt_install azure-cli
      ;;
    rhel)
      need_sudo
      run_shell 'sudo rpm --import https://packages.microsoft.com/keys/microsoft.asc && sudo '"${pkg_manager}"' install -y https://packages.microsoft.com/config/rhel/'"${rhel_major}"'.0/packages-microsoft-prod.rpm'
      dnf_install azure-cli
      ;;
  esac
}

install_libvirt() {
  case "$platform" in
    debian) apt_install qemu-kvm libvirt-daemon-system libvirt-clients virtinst genisoimage ;;
    rhel) dnf_install qemu-kvm libvirt virt-install genisoimage ;;
  esac
  run sudo systemctl enable --now libvirtd
}

install_ui() {
  case "$platform" in
    macos) run brew install go ;;
    debian) apt_install golang-go ;;
    rhel) dnf_install golang ;;
  esac
  if ! "$dry_run"; then
    check_minimum_version Go 1.22 "$(go version 2>&1)"
  fi
}

install_common
install_ansible_collections
"$want_docker" && install_docker
"$want_aws" && install_aws
"$want_gcp" && install_gcp
"$want_azure" && install_azure
"$want_libvirt" && install_libvirt
"$want_ui" && install_ui

note "Installed requested prerequisites for ${platform}."
if "$want_docker" && [[ "$platform" == "macos" ]]; then
  note "Start Docker Desktop and wait for its daemon before using the Docker target."
elif "$want_docker"; then
  note "Run 'sudo usermod -aG docker $USER', then start a new login session before using Docker without sudo."
fi
if "$want_libvirt"; then
  note "Run 'sudo usermod -aG libvirt $USER', then start a new login session before using Libvirt."
fi
note "Configure cloud credentials, SSH keys, and the CHAOS provider separately; see README.md."
