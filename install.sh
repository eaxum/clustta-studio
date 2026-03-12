#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────
# Clustta Studio — one-line installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/eaxum/clustta-studio/main/install.sh | bash
#
# Or with options:
#   curl -fsSL ... | bash -s -- --private --dir /opt/clustta
# ─────────────────────────────────────────────────────────────

CLUSTTA_VERSION="${CLUSTTA_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/clustta-studio}"
COMPOSE_URL="https://raw.githubusercontent.com/eaxum/clustta-studio/main/deploy/docker-compose.yml"
COMPOSE_TRAEFIK_URL="https://raw.githubusercontent.com/eaxum/clustta-studio/main/deploy/docker-compose.traefik.yml"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

print_banner() {
  echo -e "${CYAN}"
  echo "  ╔═══════════════════════════════════════╗"
  echo "  ║         Clustta Studio Installer       ║"
  echo "  ╚═══════════════════════════════════════╝"
  echo -e "${NC}"
}

info()    { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# ── Parse arguments ──────────────────────────────────────────
PRIVATE_MODE=false
USE_TRAEFIK=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --private)   PRIVATE_MODE=true; shift ;;
    --traefik)   USE_TRAEFIK=true; shift ;;
    --dir)       INSTALL_DIR="$2"; shift 2 ;;
    --version)   CLUSTTA_VERSION="$2"; shift 2 ;;
    --help|-h)
      echo "Usage: install.sh [OPTIONS]"
      echo ""
      echo "Options:"
      echo "  --private       Run in private mode (no global Clustta server)"
      echo "  --traefik       Include Traefik reverse proxy with auto-TLS"
      echo "  --dir PATH      Installation directory (default: ~/clustta-studio)"
      echo "  --version VER   Docker image version tag (default: latest)"
      echo "  -h, --help      Show this help"
      exit 0
      ;;
    *) error "Unknown option: $1"; exit 1 ;;
  esac
done

# ── Check / install Docker ──────────────────────────────────
check_docker() {
  if command -v docker &>/dev/null; then
    info "Docker found: $(docker --version)"
    return 0
  fi
  return 1
}

install_docker() {
  info "Installing Docker..."

  if [[ -f /etc/os-release ]]; then
    . /etc/os-release
    case "$ID" in
      ubuntu|debian|pop|linuxmint)
        sudo apt-get update -qq
        sudo apt-get install -y -qq apt-transport-https ca-certificates curl software-properties-common
        curl -fsSL "https://download.docker.com/linux/$ID/gpg" | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/$ID $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
        sudo apt-get update -qq
        sudo apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin
        ;;
      fedora|centos|rhel|rocky|alma)
        sudo dnf -y install dnf-plugins-core
        sudo dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo 2>/dev/null || \
        sudo dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
        sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
        ;;
      *)
        warn "Unsupported distro '$ID'. Trying Docker's convenience script..."
        curl -fsSL https://get.docker.com | sudo sh
        ;;
    esac
  else
    warn "Cannot detect OS. Trying Docker's convenience script..."
    curl -fsSL https://get.docker.com | sudo sh
  fi

  sudo systemctl enable --now docker
  sudo usermod -aG docker "$USER"
  info "Docker installed successfully."
  warn "You may need to log out and back in for Docker group membership to take effect."
}

# ── Check Docker Compose ────────────────────────────────────
check_compose() {
  if docker compose version &>/dev/null; then
    info "Docker Compose found: $(docker compose version --short)"
    return 0
  fi
  return 1
}

# ── Interactive configuration ────────────────────────────────
configure_env() {
  local env_file="$INSTALL_DIR/.env"

  info "Configuring Clustta Studio..."
  echo ""

  # Data directories (default to subdirs of install dir)
  local data_folder="$INSTALL_DIR/data"
  local projects_folder="$INSTALL_DIR/projects"

  echo -e "${BOLD}Data directory${NC} [${data_folder}]: "
  read -r input
  data_folder="${input:-$data_folder}"

  echo -e "${BOLD}Projects directory${NC} [${projects_folder}]: "
  read -r input
  projects_folder="${input:-$projects_folder}"

  mkdir -p "$data_folder" "$projects_folder"

  # Private mode
  if [[ "$PRIVATE_MODE" != true ]]; then
    echo ""
    echo -e "${BOLD}Connect to Clustta Cloud?${NC} (y/N): "
    read -r cloud_choice
    if [[ "$cloud_choice" =~ ^[Yy]$ ]]; then
      PRIVATE_MODE=false
    else
      PRIVATE_MODE=true
    fi
  fi

  local api_key=""
  local server_name=""
  local server_url=""

  if [[ "$PRIVATE_MODE" != true ]]; then
    echo -e "${BOLD}Studio API Key${NC}: "
    read -r api_key
    echo -e "${BOLD}Studio Name${NC}: "
    read -r server_name

    # Auto-detect host IP for server URL
    local host_ip
    host_ip=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "0.0.0.0")
    local default_url="http://${host_ip}/clustta"
    echo -e "${BOLD}Server URL${NC} [${default_url}]: "
    read -r input
    server_url="${input:-$default_url}"
  fi

  # Write .env file
  cat > "$env_file" <<EOF
# Clustta Studio Configuration
# Generated by install.sh on $(date -u +"%Y-%m-%dT%H:%M:%SZ")

DATA_FOLDER=${data_folder}
PROJECTS_FOLDER=${projects_folder}

# Database paths
STUDIO_USERS_DB=/var/data/studio_users.db
SESSION_DB=/var/data/sessions.db

# Private mode
PRIVATE=${PRIVATE_MODE}
EOF

  if [[ "$PRIVATE_MODE" != true ]]; then
    cat >> "$env_file" <<EOF

# Clustta Cloud connection
CLUSTTA_STUDIO_API_KEY=${api_key}
CLUSTTA_SERVER_NAME=${server_name}
CLUSTTA_SERVER_URL=${server_url}
EOF
  fi

  info "Configuration saved to ${env_file}"
}

# ── Detect piped input (non-interactive) ────────────────────
is_interactive() {
  [[ -t 0 ]]
}

configure_env_noninteractive() {
  local env_file="$INSTALL_DIR/.env"

  local data_folder="$INSTALL_DIR/data"
  local projects_folder="$INSTALL_DIR/projects"

  mkdir -p "$data_folder" "$projects_folder"

  cat > "$env_file" <<EOF
# Clustta Studio Configuration
# Generated by install.sh on $(date -u +"%Y-%m-%dT%H:%M:%SZ")

DATA_FOLDER=${data_folder}
PROJECTS_FOLDER=${projects_folder}

# Database paths
STUDIO_USERS_DB=/var/data/studio_users.db
SESSION_DB=/var/data/sessions.db

# Private mode (edit this file to configure Clustta Cloud connection)
PRIVATE=true
EOF

  info "Default configuration written to ${env_file}"
  warn "Edit ${env_file} to customize settings before starting."
}

# ── Main ─────────────────────────────────────────────────────
main() {
  print_banner

  # 1. Docker
  if ! check_docker; then
    install_docker
    if ! check_docker; then
      error "Docker installation failed. Please install Docker manually:"
      echo "  https://docs.docker.com/engine/install/"
      exit 1
    fi
  fi

  # 2. Docker Compose
  if ! check_compose; then
    error "Docker Compose plugin not found. Please install it:"
    echo "  https://docs.docker.com/compose/install/"
    exit 1
  fi

  # 3. Create install directory
  info "Installing to ${INSTALL_DIR}"
  mkdir -p "$INSTALL_DIR"

  # 4. Download compose file
  if [[ "$USE_TRAEFIK" == true ]]; then
    info "Downloading docker-compose.yml (with Traefik)..."
    curl -fsSL "$COMPOSE_TRAEFIK_URL" -o "$INSTALL_DIR/docker-compose.yml"
  else
    info "Downloading docker-compose.yml..."
    curl -fsSL "$COMPOSE_URL" -o "$INSTALL_DIR/docker-compose.yml"
  fi

  # 5. Pin version in compose file
  if [[ "$CLUSTTA_VERSION" != "latest" ]]; then
    sed -i "s|eaxum/clustta:latest|eaxum/clustta:${CLUSTTA_VERSION}|g" "$INSTALL_DIR/docker-compose.yml"
  fi

  # 6. Configure environment
  if is_interactive; then
    configure_env
  else
    configure_env_noninteractive
  fi

  # 7. Start
  info "Starting Clustta Studio..."
  cd "$INSTALL_DIR"
  docker compose up -d

  echo ""
  echo -e "${GREEN}${BOLD}  ✓ Clustta Studio is running!${NC}"
  echo ""
  echo -e "  ${BOLD}Directory:${NC}   ${INSTALL_DIR}"

  # Determine port
  local port="7774"
  if [[ "$USE_TRAEFIK" == true ]]; then
    port="80"
  fi

  local host_ip
  host_ip=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "localhost")
  echo -e "  ${BOLD}URL:${NC}         http://${host_ip}:${port}"
  echo ""
  echo -e "  Manage with:"
  echo -e "    cd ${INSTALL_DIR}"
  echo -e "    docker compose logs -f    ${CYAN}# view logs${NC}"
  echo -e "    docker compose restart    ${CYAN}# restart${NC}"
  echo -e "    docker compose down       ${CYAN}# stop${NC}"
  echo ""
}

main "$@"
