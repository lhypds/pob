//go:build windows

// The Windows half of `pob update`: the release zip, and the install.ps1 that
// comes inside it run over this install — what a user downloading the zip from
// the releases page would do by hand. There is no get.sh here to hand the job
// to, so the two steps get.sh would have done (fetch, unpack) are done here and
// the installer from the release does the rest, as it does everywhere else.
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// runUpdate downloads the release and installs it over this one.
func runUpdate(target, prefix, _ string) {
	zipName := fmt.Sprintf("Pob-%s-windows-%s.zip", target, runtime.GOARCH)
	zipURL := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repoSlug, target, zipName)

	tmp, err := os.MkdirTemp("", "pob-update-*")
	if err != nil {
		fail("cannot make a temporary directory: %v", err)
	}
	defer os.RemoveAll(tmp)

	archivePath := filepath.Join(tmp, zipName)
	if err := download(zipURL, archivePath, fmt.Sprintf("⬇️  Downloading Pob %s for windows/%s", target, runtime.GOARCH)); err != nil {
		fail("could not download %s: %v\n      %s\n"+
			"      Check that version %s exists: https://github.com/%s/releases",
			zipName, err, zipURL, target, repoSlug)
	}

	fmt.Println("📂 Unpacking…")
	if err := unzip(archivePath, tmp); err != nil {
		fail("could not unpack %s: %v", zipName, err)
	}

	src := filepath.Join(tmp, "Pob")
	installer := filepath.Join(src, "install.ps1")
	for _, needed := range []string{"Pob.exe", "pob-core.exe", `Helpers\pob.exe`, "install.ps1"} {
		if !exists(filepath.Join(src, needed)) {
			fail("%s did not contain what was expected (no %s) — nothing installed", zipName, needed)
		}
	}

	// Windows will not let a running .exe be overwritten, and the .exe running
	// is this one — the installer's copy into Helpers\ would fail partway
	// through everything else it had already replaced. Renaming a running image
	// *is* allowed, though, and it keeps running from the renamed file: this
	// process finishes out of pob.exe.old while the new pob.exe is written
	// beside it. Nothing can delete it until it exits, so it is the next update
	// that sweeps it up.
	restore := stepAside(prefix)

	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		powershell = "powershell"
	}
	fmt.Println()
	cmd := exec.Command(powershell, "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", installer, "-InstallDir", prefix)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		// The installer said why on its way out; what it could not say is that
		// the CLI it was to replace has been moved aside, so put it back.
		restore()
		fail("the installer did not finish: %v", err)
	}
}

// stepAside renames this CLI out of the way when it is the copy the install is
// about to overwrite, and returns the undo for a failed install. Both are
// nothing at all when this pob is not in the tree being installed to — a `pob`
// run from somewhere else updating this install is not in anyone's way.
func stepAside(prefix string) (restore func()) {
	exe, err := os.Executable()
	if err != nil {
		return func() {}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !strings.EqualFold(filepath.Dir(exe), filepath.Join(prefix, "Helpers")) {
		return func() {}
	}

	aside := exe + ".old"
	// Whatever the last update left behind; it is not running now, so this is
	// the moment it can go.
	_ = os.Remove(aside)
	if err := os.Rename(exe, aside); err != nil {
		// Say it and carry on: the installer may still manage the copy, and if
		// it cannot, its own error is the one worth reading.
		fmt.Printf("⚠️  Could not move %s aside: %v\n", exe, err)
		return func() {}
	}
	return func() {
		if err := os.Rename(aside, exe); err != nil {
			fmt.Printf("⚠️  %s is now at %s and could not be put back: %v\n", exe, aside, err)
		}
	}
}

// download fetches url to path, reporting progress on one rewritten line when
// the server says how big the file is.
func download(url, path, label string) error {
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	progress := &progressWriter{label: label, total: resp.ContentLength, last: time.Now()}
	if _, err := io.Copy(io.MultiWriter(file, progress), resp.Body); err != nil {
		progress.done()
		return err
	}
	progress.done()
	if progress.written == 0 {
		return fmt.Errorf("the download was empty")
	}
	return file.Close()
}

// progressWriter counts bytes on their way to disk and prints how far along
// they are, no more than a few times a second.
type progressWriter struct {
	label   string
	total   int64
	written int64
	last    time.Time
}

func (p *progressWriter) Write(data []byte) (int, error) {
	p.written += int64(len(data))
	if time.Since(p.last) >= 200*time.Millisecond {
		p.last = time.Now()
		p.print()
	}
	return len(data), nil
}

func (p *progressWriter) print() {
	megabytes := float64(p.written) / (1 << 20)
	if p.total > 0 {
		fmt.Printf("\r%s… %d%% (%.0f MB)", p.label, p.written*100/p.total, megabytes)
		return
	}
	fmt.Printf("\r%s… %.0f MB", p.label, megabytes)
}

// done leaves the finished line on screen and moves off it, so whatever prints
// next does not land on top of the progress.
func (p *progressWriter) done() {
	p.print()
	fmt.Println()
}

// unzip unpacks a release zip into dir.
func unzip(archivePath, dir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, entry := range reader.File {
		// Nothing in a Pob release climbs out of its own folder, but a zip that
		// is not one is not going to be given the chance to write anywhere else.
		path := filepath.Join(dir, filepath.FromSlash(entry.Name))
		if !strings.HasPrefix(path, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("%s is outside the archive", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeZipEntry(entry, path); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(entry *zip.File, path string) error {
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
