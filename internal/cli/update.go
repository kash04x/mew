package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	if err := downloadAndReplace(downloadURL, self); err != nil {
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

func downloadAndReplace(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	// Write to a temp file in the destination directory so the rename is
	// atomic and stays on the same filesystem.
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".mew-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed

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
	return os.Rename(tmpPath, dest)
}
