package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// githubRepo is the "owner/name" that releases are published under. Update
// this if the project moves.
const githubRepo = "kash04x/mew"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update mew to the latest release",
	Long: `Checks GitHub for the latest release. If a newer version is available,
downloads the binary for your platform and replaces the current installation
in place.`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	current := rootCmd.Version

	fmt.Fprintf(out, "Current version : %s\n", current)
	fmt.Fprintln(out, "Checking latest release...")

	latest, downloadURL, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("could not fetch release info: %w", err)
	}
	fmt.Fprintf(out, "Latest version  : %s\n", latest)

	if current == latest {
		fmt.Fprintln(out, "Already up to date.")
		return nil
	}
	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, latest)
	}

	self, err := resolvedBinaryPath()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Downloading %s...\n", latest)
	if err := downloadAndReplace(out, downloadURL, self); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Fprintf(out, "Updated to %s. Run `mew version` to confirm.\n", latest)
	return nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchLatestRelease() (version, downloadURL string, err error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}

	assetName := fmt.Sprintf("mew-%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		if a.Name == assetName {
			return rel.TagName, a.BrowserDownloadURL, nil
		}
	}
	return rel.TagName, "", nil
}

func downloadAndReplace(out io.Writer, url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	// Prefer a temp file beside the destination so the swap is an atomic,
	// same-filesystem rename (also the safe way to replace a running binary).
	// If the destination directory is not writable — the usual case for a
	// root-owned /usr/local/bin — stage the download in the system temp dir
	// and install the final binary through sudo instead.
	dir := filepath.Dir(dest)
	elevate := false
	tmp, err := os.CreateTemp(dir, ".mew-update-*")
	if errors.Is(err, fs.ErrPermission) {
		elevate = true
		tmp, err = os.CreateTemp("", ".mew-update-*")
	}
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once moved into place

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}

	if elevate {
		return sudoInstall(out, tmpPath, dest)
	}
	return os.Rename(tmpPath, dest)
}

// sudoInstall places src at dest with elevated privileges, prompting for the
// password on the terminal. Only the final install step runs as root — the
// download already happened as the current user.
func sudoInstall(out io.Writer, src, dest string) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("writing %s needs elevated permissions, but sudo was not found; re-run as root (sudo mew update) or install mew somewhere writable", dest)
	}
	fmt.Fprintf(out, "Writing to %s needs elevated permissions — you may be prompted for your password.\n", filepath.Dir(dest))

	cmd := exec.Command(sudo, "install", "-m", "0755", src, dest)
	// Wire the real terminal so sudo can read the password and show its prompt.
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo install failed: %w", err)
	}
	return nil
}
