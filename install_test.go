package agents_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallScriptInstallsSelectedVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh tests require a POSIX shell")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	tools := filepath.Join(dir, "tools")
	installDir := filepath.Join(dir, "install")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "agents"), []byte("old binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTool(t, tools, "uname", `#!/bin/sh
case "$1" in
  -s) printf 'Linux' ;;
  -m) printf 'x86_64' ;;
esac
`)
	writeTool(t, tools, "curl", `#!/bin/sh
out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
case "$url" in
  */checksums.txt) printf 'fakehash  agents_1.2.3_linux_amd64.tar.gz\n' > "$out" ;;
  */agents_1.2.3_linux_amd64.tar.gz) printf 'archive' > "$out" ;;
  *) echo "unexpected curl url: $url" >&2; exit 1 ;;
esac
`)
	writeTool(t, tools, "sha256sum", `#!/bin/sh
grep 'fakehash  agents_1.2.3_linux_amd64.tar.gz' "$2" >/dev/null
`)
	writeTool(t, tools, "tar", `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -C) dir=$2; shift 2 ;;
    *) shift ;;
  esac
done
printf '#!/bin/sh\necho agents fake\n' > "$dir/agents"
chmod 755 "$dir/agents"
`)

	cmd := exec.Command("sh", "./install.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+tools+string(os.PathListSeparator)+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENTS_VERSION=1.2.3",
		"AGENTS_INSTALL_DIR="+installDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "installed agents v1.2.3 to "+installDir+"/agents") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	installed, err := os.ReadFile(filepath.Join(installDir, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installed), "agents fake") {
		t.Fatalf("installer did not place fake binary, got:\n%s", installed)
	}
}

func TestInstallScriptResolvesLatestAndUsesUserBin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh tests require a POSIX shell")
	}

	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTool(t, tools, "uname", `#!/bin/sh
case "$1" in
  -s) printf 'Darwin' ;;
  -m) printf 'arm64' ;;
esac
`)
	writeTool(t, tools, "curl", `#!/bin/sh
effective=0
out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -w) effective=1; shift 2 ;;
    -o) out=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
if [ "$effective" = 1 ]; then
  printf 'https://github.com/mekilis/agents/releases/tag/v2.0.0'
  exit 0
fi
case "$url" in
  */v2.0.0/checksums.txt) printf 'fakehash  agents_2.0.0_darwin_arm64.tar.gz\n' > "$out" ;;
  */v2.0.0/agents_2.0.0_darwin_arm64.tar.gz) printf 'archive' > "$out" ;;
  *) echo "unexpected curl url: $url" >&2; exit 1 ;;
esac
`)
	writeTool(t, tools, "shasum", `#!/bin/sh
case "$1 $2 $3" in
  "-a 256 -c") grep 'fakehash  agents_2.0.0_darwin_arm64.tar.gz' "$4" >/dev/null ;;
  *) exit 1 ;;
esac
`)
	writeTool(t, tools, "sha256sum", `#!/bin/sh
grep 'fakehash  agents_2.0.0_darwin_arm64.tar.gz' "$2" >/dev/null
`)
	writeTool(t, tools, "tar", `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -C) dir=$2; shift 2 ;;
    *) shift ;;
  esac
done
printf 'latest binary\n' > "$dir/agents"
chmod 755 "$dir/agents"
`)

	cmd := exec.Command("sh", "./install.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+testPath(tools),
		"HOME="+home,
		"AGENTS_VERSION=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	want := filepath.Join(home, ".local", "bin", "agents")
	if !strings.Contains(string(out), "installed agents v2.0.0 to "+want) {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "add "+filepath.Dir(want)+" to PATH") {
		t.Fatalf("expected PATH hint, got:\n%s", out)
	}
}

func TestInstallScriptReplacesExistingAgentsOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh tests require a POSIX shell")
	}

	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	activeBin := filepath.Join(dir, "active-bin")
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(activeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeBin, "agents"), []byte("old active binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTool(t, tools, "uname", `#!/bin/sh
case "$1" in
  -s) printf 'Linux' ;;
  -m) printf 'aarch64' ;;
esac
`)
	writeTool(t, tools, "curl", `#!/bin/sh
out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
case "$url" in
  */checksums.txt) printf 'fakehash  agents_3.1.4_linux_arm64.tar.gz\n' > "$out" ;;
  */agents_3.1.4_linux_arm64.tar.gz) printf 'archive' > "$out" ;;
  *) echo "unexpected curl url: $url" >&2; exit 1 ;;
esac
`)
	writeTool(t, tools, "sha256sum", `#!/bin/sh
grep 'fakehash  agents_3.1.4_linux_arm64.tar.gz' "$2" >/dev/null
`)
	writeTool(t, tools, "tar", `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -C) dir=$2; shift 2 ;;
    *) shift ;;
  esac
done
printf 'new active binary\n' > "$dir/agents"
chmod 755 "$dir/agents"
`)

	cmd := exec.Command("sh", "./install.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+testPath(tools, activeBin),
		"HOME="+home,
		"AGENTS_VERSION=3.1.4",
		"AGENTS_INSTALL_DIR=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "installed agents v3.1.4 to "+activeBin+"/agents") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	installed, err := os.ReadFile(filepath.Join(activeBin, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "new active binary\n" {
		t.Fatalf("active agents binary was not replaced, got:\n%s", installed)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "agents")); !os.IsNotExist(err) {
		t.Fatalf("installer wrote beside active binary, stat err: %v", err)
	}
}

func TestInstallScriptRejectsUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh tests require a POSIX shell")
	}

	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTool(t, tools, "uname", `#!/bin/sh
case "$1" in
  -s) printf 'FreeBSD' ;;
  -m) printf 'x86_64' ;;
esac
`)
	writeTool(t, tools, "tar", "#!/bin/sh\nexit 0\n")

	cmd := exec.Command("sh", "./install.sh")
	cmd.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("install.sh unexpectedly succeeded:\n%s", out)
	}
	if !strings.Contains(string(out), "unsupported OS: FreeBSD") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func writeTool(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func testPath(dirs ...string) string {
	base := []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	return strings.Join(append(dirs, base...), string(os.PathListSeparator))
}
