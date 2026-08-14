// Updating an installed Pob in place (`pob update`).
//
// No installer is reimplemented here. On Linux and macOS this fetches the same
// get.sh the README pipes into sh and runs it over this install; on Windows it
// downloads the release zip and runs the install.ps1 inside it — in both cases
// the path a user would take by hand. What the command adds is the three
// things that path leaves to them: which release is the latest, which install
// this CLI belongs to, and that the app has to be closed before the files
// underneath it are replaced.
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// repoSlug is the repository the releases come from. getScriptURL is the
	// installer on its default branch — master rather than the tag being
	// installed, so an update always runs the newest installer, which is the
	// one the README's one-liner would have used.
	repoSlug     = "lhypds/pob"
	getScriptURL = "https://raw.githubusercontent.com/" + repoSlug + "/master/get.sh"
	// GitHub redirects this to the tag page of the newest release, so where it
	// lands is the version — no API call, and no JSON to read it out of.
	latestReleaseURL = "https://github.com/" + repoSlug + "/releases/latest"
)

type updateOptions struct {
	check   bool
	version string
	prefix  string
	bin     string
}

// cmdUpdate replaces this install with another release of it. With nothing
// said it is the latest release; --check only says what that is.
func cmdUpdate(root string, args []string) {
	opts := parseUpdateArgs(args)

	current := strings.TrimPrefix(version, "v")
	target := opts.version
	if target == "" {
		fmt.Println("🔎 Looking up the latest release…")
		latest, err := latestVersion()
		if err != nil {
			fail("could not work out the latest release: %v\n"+
				"      Check the network, or name one yourself: pob update --version 0.2.3", err)
		}
		target = latest
	}

	if opts.check {
		reportUpdate(current, target)
		return
	}

	// A version asked for by name is an instruction, not a question: it is how
	// a release is reinstalled over itself, and how an older one is gone back
	// to. Only the implicit "latest" has nothing to do when it is what's here.
	if current == target && opts.version == "" {
		fmt.Printf("Pob %s is the latest release — nothing to update.\n", current)
		fmt.Printf("Reinstall it anyway with: pob update --version %s\n", target)
		return
	}

	// The app that is being replaced is the one running: on Windows its files
	// cannot be overwritten at all, and everywhere else it would be left as a
	// live process with a different install underneath it.
	if inst := theInstance(root); inst.Running {
		fail("Pob is running (instance %s) — quit it first (`pob kill`), then update", inst.ID)
	}

	prefix := opts.prefix
	if prefix == "" {
		located, err := installLocation()
		if err != nil {
			fail("%v", err)
		}
		prefix = located
	}

	fmt.Printf("⬆️  Updating Pob %s → %s in %s…\n", currentLabel(current), target, prefix)
	runUpdate(target, prefix, opts.bin)
}

// parseUpdateArgs reads the options `pob update` takes; the three that are not
// --check are the ones get.sh takes, and are passed on to it.
func parseUpdateArgs(args []string) updateOptions {
	var opts updateOptions
	for len(args) > 0 {
		switch args[0] {
		case "--check":
			opts.check = true
			args = args[1:]
		case "--version":
			if len(args) < 2 {
				fail("--version needs a version, e.g. `pob update --version 0.2.3`")
			}
			opts.version = strings.TrimPrefix(args[1], "v")
			args = args[2:]
		case "--prefix":
			if len(args) < 2 {
				fail("--prefix needs a directory")
			}
			opts.prefix = args[1]
			args = args[2:]
		case "--bin":
			if len(args) < 2 {
				fail("--bin needs a directory")
			}
			if runtime.GOOS == "windows" {
				fail("--bin is not a Windows option — the installer puts the CLI in <install>\\Helpers and that on your PATH")
			}
			opts.bin = args[1]
			args = args[2:]
		default:
			fail("unknown update option %q — run `pob help`", args[0])
		}
	}
	return opts
}

// reportUpdate is `pob update --check`: what is installed, what is out there,
// and nothing touched. It exits 1 when there is a newer release, the same way
// `macro --check` exits 1 when there is something to fix — so a script can ask
// this question without reading the text.
func reportUpdate(current, latest string) {
	fmt.Printf("Installed:  %s\n", currentLabel(current))
	fmt.Printf("Latest:     %s\n", latest)
	fmt.Println()

	switch {
	case current == latest:
		fmt.Println("Pob is up to date.")
	case !isRelease(current):
		// A build from the repository has no version to compare, so what the
		// update would do is said instead of guessed at.
		fmt.Println("This is a build from the repository, not a release — an update would put")
		fmt.Printf("release %s over it.\n", latest)
		os.Exit(1)
	case newerRelease(current, latest):
		fmt.Println("Update with: pob update")
		os.Exit(1)
	default:
		// A build of a tag that is not released yet, or an older release named
		// by hand as the latest: either way there is nothing newer to fetch.
		fmt.Printf("What is installed is newer than the latest release — nothing to update.\n")
	}
}

// currentLabel names the running version for the messages above: the version
// itself for a release, and what a build from the repository is for anything
// else, since "dev" on its own reads like a version.
func currentLabel(current string) string {
	if isRelease(current) {
		return current
	}
	return current + " (a build from the repository)"
}

// latestVersion is the version of the newest GitHub release, read off the tag
// page /releases/latest redirects to.
func latestVersion() (string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	// HEAD, not GET: the answer is the URL this ends up at, not the page.
	resp, err := client.Head(latestReleaseURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, latestReleaseURL)
	}

	// resp.Request is the last request the client made, so its URL is where the
	// redirects ended — GitHub's tag page for the newest release.
	landed := resp.Request.URL.String()
	_, tag, found := strings.Cut(landed, "/releases/tag/")
	if !found || tag == "" {
		return "", fmt.Errorf("%s did not lead to a release tag (%s)", latestReleaseURL, landed)
	}
	return strings.TrimPrefix(tag, "v"), nil
}

// isRelease reports whether a version string is a release number — 0.2.3 and
// not "dev", which is what an unstamped build of the CLI calls itself.
func isRelease(v string) bool {
	if v == "" {
		return false
	}
	for _, part := range strings.Split(v, ".") {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

// newerRelease reports whether latest is a later release than current,
// comparing the dot-separated numbers so 0.2.10 is after 0.2.9.
func newerRelease(current, latest string) bool {
	a, b := versionParts(current), versionParts(latest)
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return y > x
		}
	}
	return false
}

// versionParts reads a version as its numbers; anything that is not one counts
// as 0, so a version this does not understand simply sorts early.
func versionParts(v string) []int {
	fields := strings.Split(v, ".")
	parts := make([]int, len(fields))
	for i, field := range fields {
		parts[i], _ = strconv.Atoi(field)
	}
	return parts
}

// installLocation is the install this CLI belongs to, named the way the
// installer names it: the folder Pob.app sits in on macOS, and the app tree
// itself on Linux and Windows. An update has to be pointed back at it, or it
// installs a second copy in the default place and leaves this one behind.
//
// The CLI is in a Helpers directory beside the app in every install — the same
// fact `pob launch` finds the app by — so a pob that is not in one is a build
// in a checkout, which is not something an update can replace.
func installLocation() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot tell where this pob is installed: %v", err)
	}
	// The command on the PATH is a symlink into the install on macOS and Linux;
	// the install is where it points, not where the link is.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	notAnInstall := fmt.Errorf("this pob is a build in %s, not an install — there is nothing here for\n"+
		"      `pob update` to replace. In a checkout, update it with `git pull` and\n"+
		"      ./setup.sh; to install a release over it anyway, name the place:\n"+
		"      pob update --prefix DIR", dir)

	if filepath.Base(dir) != "Helpers" {
		return "", notAnInstall
	}
	parent := filepath.Dir(dir)
	if runtime.GOOS == "darwin" {
		// Pob.app/Contents/Helpers/pob — what the installer is given is the
		// folder the bundle goes in, /Applications or ~/Applications.
		bundle := filepath.Dir(parent)
		if !strings.HasSuffix(bundle, ".app") {
			return "", notAnInstall
		}
		return filepath.Dir(bundle), nil
	}
	// <install>/Helpers/pob — the app sits beside Helpers, so the install is
	// the directory holding both.
	return parent, nil
}
