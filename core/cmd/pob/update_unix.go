//go:build !windows

// The Linux and macOS half of `pob update`: the repository's own get.sh, run
// over this install.
//
// It is exec'd rather than started as a child, so nothing of this CLI is left
// running while the install it lives in is taken apart underneath it — from
// there on the shell *is* this process, and its output and its exit status are
// the command's own.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// runUpdate hands this install to get.sh. It does not return: either the
// installer has replaced this process, or something before that has failed.
func runUpdate(target, prefix, binDir string) {
	// Said before anything is downloaded: an install into a directory this user
	// cannot write is a wall of "Permission denied" halfway through otherwise,
	// and the answer to it — the same command under sudo — is not obvious from
	// the errors the installer would print.
	if !writable(prefix) {
		fail("cannot write %s — that install belongs to root:\n"+
			"      sudo pob update", prefix)
	}

	fmt.Println("⬇️  Fetching the installer…")
	script, err := fetchInstaller()
	if err != nil {
		fail("could not fetch the installer: %v\n      %s", err, getScriptURL)
	}

	sh, err := exec.LookPath("sh")
	if err != nil {
		sh = "/bin/sh"
	}
	argv := []string{"sh", script, "--version", target, "--prefix", prefix}
	if binDir != "" {
		argv = append(argv, "--bin", binDir)
	}
	fmt.Println()

	if err := syscall.Exec(sh, argv, os.Environ()); err != nil {
		fail("could not run the installer: %v", err)
	}
}

// fetchInstaller downloads get.sh to a temporary file and returns its path.
//
// Nothing deletes that file: the process is replaced by the shell reading it,
// so there is no later here to clean up in, and the temporary directory is
// swept by the system. get.sh's own downloads do clean up after themselves —
// it traps its exit, and that exit is this command's.
func fetchInstaller() (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(getScriptURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	// A proxy's error page is a 200 with prose in it, and prose handed to sh
	// runs as far as its first line that happens to parse.
	if !strings.HasPrefix(string(body), "#!") {
		return "", fmt.Errorf("what came back is not a shell script")
	}

	file, err := os.CreateTemp("", "pob-update-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return file.Name(), nil
}

// writable reports whether this user can install into dir. A directory that is
// not there yet is judged by the nearest ancestor that is, since that is what
// the installer has to create it in.
func writable(dir string) bool {
	for {
		if _, err := os.Stat(dir); err == nil {
			return syscall.Access(dir, 0x2 /* W_OK */) == nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
