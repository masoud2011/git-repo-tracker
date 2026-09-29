package macos_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallReplacesEntireBundle(t *testing.T) {
	requireMacOS(t)
	root := t.TempDir()
	out := filepath.Join(root, "build output")
	installDir := filepath.Join(root, "Applications with spaces")
	app := filepath.Join(installDir, "git-repo-tracker.app")
	packageApp(t, out, installDir, "1.0.0", nil, false)
	marker := filepath.Join(app, "Contents", "obsolete-resource")
	writeFile(t, marker, "old resource", 0o644)
	packageApp(t, out, installDir, "2.0.0", nil, false)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("obsolete resource survived replacement: %v", err)
	}
	plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil || !bytes.Contains(plist, []byte("<string>2.0.0</string>")) {
		t.Fatalf("new version was not installed: %s, %v", plist, err)
	}
	for _, name := range []string{"Contents/MacOS/git-repo-tracker", "Contents/Resources/icon.icns"} {
		built, err := os.ReadFile(filepath.Join(out, "git-repo-tracker.app", name))
		if err != nil {
			t.Fatal(err)
		}
		installed, err := os.ReadFile(filepath.Join(app, name))
		if err != nil || !bytes.Equal(built, installed) {
			t.Fatalf("installed %s differs from build: %v", name, err)
		}
	}
	if output, err := exec.Command("codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		t.Fatalf("invalid installed signature: %s: %v", output, err)
	}
}

func TestFailedInstallPreservesPreviousBundle(t *testing.T) {
	requireMacOS(t)
	for _, tc := range []struct {
		name, tool, script string
	}{
		{"signing", "codesign", "exit 42\n"},
		{"copying", "ditto", "exit 42\n"},
		{"promotion", "mv", "case \"$1\" in */.git-repo-tracker-install.*/git-repo-tracker.app) exit 42;; esac\nexec /bin/mv \"$@\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			installDir := filepath.Join(root, "Applications")
			app := filepath.Join(installDir, "git-repo-tracker.app")
			marker := filepath.Join(app, "Contents", "previous-version")
			writeFile(t, marker, "keep this app", 0o644)
			bin := filepath.Join(root, "tools")
			writeFile(t, filepath.Join(bin, tc.tool), "#!/bin/sh\n"+tc.script, 0o755)
			packageApp(t, filepath.Join(root, "output"), installDir, "2.0.0",
				[]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}, true)
			content, err := os.ReadFile(marker)
			if err != nil || string(content) != "keep this app" {
				t.Fatalf("previous app was not preserved: %q, %v", content, err)
			}
			entries, err := os.ReadDir(installDir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "git-repo-tracker.app" {
				t.Fatalf("temporary install files were not cleaned up: %v, %v", entries, err)
			}
		})
	}
}

func TestInstallRejectsOutputDirectoryAsDestination(t *testing.T) {
	requireMacOS(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "git-repo-tracker.app", "previous-version")
	writeFile(t, marker, "keep this app", 0o644)
	packageApp(t, dir, dir, "2.0.0", nil, true)
	if content, err := os.ReadFile(marker); err != nil || string(content) != "keep this app" {
		t.Fatalf("rejected install destroyed the old app: %q, %v", content, err)
	}
}

func TestOverlappingPackagesCannotChangeEachOthersBundle(t *testing.T) {
	requireMacOS(t)
	root := t.TempDir()
	out := filepath.Join(root, "output")
	installDir := filepath.Join(root, "Applications")
	ready, release := filepath.Join(root, "ready"), filepath.Join(root, "release")
	bin := filepath.Join(root, "tools")
	writeFile(t, filepath.Join(bin, "codesign"), `#!/bin/sh
if [ "$1" = --force ]; then
  touch "$PACKAGE_READY"
  while [ ! -e "$PACKAGE_RELEASE" ]; do sleep 0.01; done
fi
exec /usr/bin/codesign "$@"
`, 0o755)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	first := exec.Command("bash", "package-app.sh", "--binary", binary, "--version", "1.0.0",
		"--out", out, "--install", installDir)
	first.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PACKAGE_READY="+ready, "PACKAGE_RELEASE="+release)
	var output bytes.Buffer
	first.Stdout, first.Stderr = &output, &output
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		writeFile(t, release, "", 0o644)
		if err := first.Wait(); err != nil {
			t.Errorf("first package failed: %s: %v", output.String(), err)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first package did not reach signing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	packageApp(t, out, installDir, "2.0.0", nil, true)
	plist, err := os.ReadFile(filepath.Join(out, "git-repo-tracker.app", "Contents", "Info.plist"))
	if err != nil || !bytes.Contains(plist, []byte("<string>1.0.0</string>")) {
		t.Fatalf("second package changed the first bundle: %s: %v", plist, err)
	}
}

func TestBuildFailureDoesNotReplaceInstalledApp(t *testing.T) {
	requireMacOS(t)
	root := t.TempDir()
	installDir := filepath.Join(root, "Applications")
	marker := filepath.Join(installDir, "git-repo-tracker.app", "previous-version")
	writeFile(t, marker, "keep this app", 0o644)
	bin := filepath.Join(root, "tools")
	writeFile(t, filepath.Join(bin, "go"), "#!/bin/sh\nexit 42\n", 0o755)
	cmd := exec.Command("bash", copyBuildScript(t, root), filepath.Join(root, "binary"))
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_REPO_TRACKER_INSTALL_DIR="+installDir)
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
		t.Fatalf("expected compiler failure to stop the build: %s: %v", output, err)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "keep this app" {
		t.Fatalf("failed build changed the installed app: %q, %v", content, err)
	}
}

func TestOverlappingBuildStopsBeforeCompiler(t *testing.T) {
	requireMacOS(t)
	root := t.TempDir()
	script := copyBuildScript(t, root)
	ready, release := filepath.Join(root, "ready"), filepath.Join(root, "release")
	bin := filepath.Join(root, "tools")
	writeFile(t, filepath.Join(bin, "go"), `#!/bin/sh
if [ "$BLOCK_BUILD" = yes ]; then
  touch "$BUILD_READY"
  while [ ! -e "$BUILD_RELEASE" ]; do sleep 0.01; done
fi
exit 42
`, 0o755)
	env := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BUILD_READY="+ready, "BUILD_RELEASE="+release)
	first := exec.Command("bash", script)
	first.Env = append(env, "BLOCK_BUILD=yes")
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		writeFile(t, release, "", 0o644)
		if exit, ok := first.Wait().(*exec.ExitError); !ok || exit.ExitCode() != 42 {
			t.Errorf("first build did not reach the compiler failure: %v", exit)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first build did not reach the compiler")
		}
		time.Sleep(10 * time.Millisecond)
	}
	second := exec.Command("bash", script)
	second.Env = append(env, "BLOCK_BUILD=no")
	output, err := second.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() == 42 {
		t.Fatalf("overlapping build reached the compiler: %s: %v", output, err)
	}
}

func copyBuildScript(t *testing.T, project string) string {
	t.Helper()
	content, err := os.ReadFile("../build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(project, "build", "build.sh")
	writeFile(t, script, string(content), 0o755)
	return script
}

func packageApp(t *testing.T, out, installDir, version string, env []string, wantFailure bool) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "package-app.sh", "--binary", binary, "--version", version,
		"--out", out, "--install", installDir)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if (err != nil) != wantFailure {
		t.Fatalf("package result (wantFailure=%t): %s: %v", wantFailure, output, err)
	}
	if strings.Contains(string(output), "unknown argument") {
		t.Fatalf("install option is not supported: %s", output)
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func requireMacOS(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS packaging requires codesign and ditto")
	}
}
