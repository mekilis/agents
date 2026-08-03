#!/bin/sh
set -eu

REPO_OWNER=${AGENTS_REPO_OWNER:-mekilis}
REPO_NAME=${AGENTS_REPO_NAME:-agents}
INSTALL_DIR=${AGENTS_INSTALL_DIR:-}
VERSION=${AGENTS_VERSION:-}

err() {
	printf 'agents installer: %s\n' "$*" >&2
}

need() {
	if ! command -v "$1" >/dev/null 2>&1; then
		err "missing required command: $1"
		exit 1
	fi
}

detect_os() {
	case "$(uname -s)" in
		Darwin) printf 'darwin' ;;
		Linux) printf 'linux' ;;
		*) err "unsupported OS: $(uname -s). Agents releases support macOS and Linux."; exit 1 ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64) printf 'amd64' ;;
		arm64|aarch64) printf 'arm64' ;;
		*) err "unsupported CPU architecture: $(uname -m). Agents releases support amd64 and arm64."; exit 1 ;;
	esac
}

download() {
	url=$1
	out=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$out"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$out"
	else
		err "curl or wget is required to download Agents releases"
		exit 1
	fi
}

latest_tag() {
	if command -v curl >/dev/null 2>&1; then
		url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO_OWNER/$REPO_NAME/releases/latest")
	elif command -v wget >/dev/null 2>&1; then
		url=$(wget -qS --spider "https://github.com/$REPO_OWNER/$REPO_NAME/releases/latest" 2>&1 | awk '/^  Location: / { loc=$2 } END { print loc }')
	else
		err "curl or wget is required to resolve the latest Agents release"
		exit 1
	fi
	tag=${url##*/}
	case "$tag" in
		v*) printf '%s' "$tag" ;;
		*) err "could not resolve latest release tag from $url"; exit 1 ;;
	esac
}

choose_install_dir() {
	if [ -n "$INSTALL_DIR" ]; then
		printf '%s' "$INSTALL_DIR"
		return
	fi

	existing=$(command -v agents 2>/dev/null || true)
	case "$existing" in
		*/*)
			if [ -f "$existing" ]; then
				printf '%s' "${existing%/*}"
				return
			fi
			;;
	esac

	if [ -n "${HOME:-}" ]; then
		for dir in "$HOME/.local/bin" "$HOME/bin"; do
			if [ -d "$dir" ] || mkdir -p "$dir" 2>/dev/null; then
				if [ -w "$dir" ]; then
					printf '%s' "$dir"
					return
				fi
			fi
		done
	fi

	printf '/usr/local/bin'
}

verify_checksum() {
	checksums=$1
	archive=$2
	name=${archive##*/}
	expected=$(grep "  $name\$" "$checksums" || true)
	if [ -z "$expected" ]; then
		err "checksums.txt does not contain $name"
		exit 1
	fi
	printf '%s\n' "$expected" > "$archive.sha256"
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "${archive%/*}" && sha256sum -c "$name.sha256")
	elif command -v shasum >/dev/null 2>&1; then
		(cd "${archive%/*}" && shasum -a 256 -c "$name.sha256")
	else
		err "sha256sum or shasum is required to verify Agents release checksums"
		exit 1
	fi
}

install_binary() {
	src=$1
	dir=$2

	if [ ! -d "$dir" ] && ! mkdir -p "$dir" 2>/dev/null; then
		err "cannot create install directory: $dir"
		err "Set AGENTS_INSTALL_DIR to a writable directory, or rerun with permissions for $dir."
		exit 1
	fi
	if [ ! -w "$dir" ]; then
		err "install directory is not writable: $dir"
		err "Set AGENTS_INSTALL_DIR to a writable directory, or rerun with permissions for $dir."
		exit 1
	fi

	tmp="$dir/.agents.$$"
	cp "$src" "$tmp"
	chmod 755 "$tmp"
	if ! mv "$tmp" "$dir/agents"; then
		rm -f "$tmp"
		err "failed to replace $dir/agents"
		exit 1
	fi
}

main() {
	need uname
	need grep
	need tar

	os=$(detect_os)
	arch=$(detect_arch)
	if [ -n "$VERSION" ]; then
		case "$VERSION" in
			v*) tag=$VERSION ;;
			*) tag="v$VERSION" ;;
		esac
	else
		tag=$(latest_tag)
	fi
	archive_version=${tag#v}
	archive_name="agents_${archive_version}_${os}_${arch}.tar.gz"
	base_url="https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$tag"

	tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/agents-install.XXXXXX")
	trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

	download "$base_url/checksums.txt" "$tmpdir/checksums.txt"
	download "$base_url/$archive_name" "$tmpdir/$archive_name"
	verify_checksum "$tmpdir/checksums.txt" "$tmpdir/$archive_name"
	tar -xzf "$tmpdir/$archive_name" -C "$tmpdir"
	if [ ! -f "$tmpdir/agents" ]; then
		err "release archive did not contain an agents binary"
		exit 1
	fi

	dir=$(choose_install_dir)
	install_binary "$tmpdir/agents" "$dir"
	printf 'installed agents %s to %s/agents\n' "$tag" "$dir"
	case ":$PATH:" in
		*":$dir:"*) ;;
		*) printf 'add %s to PATH to run agents from a new shell\n' "$dir" ;;
	esac
}

main "$@"
