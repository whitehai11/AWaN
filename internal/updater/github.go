package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

func latestRelease(repo string) (*release, error) {
	client := &http.Client{Timeout: 20 * time.Second}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "awan-updater")

	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("release lookup failed with status %s", resp.Status)
	}

	var value release
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return nil, err
	}

	return &value, nil
}

func matchingAsset(rel *release, binaryBaseName string) (*releaseAsset, error) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	candidates := []string{
		fmt.Sprintf("%s-%s-%s", binaryBaseName, osName, arch),
		fmt.Sprintf("%s-%s-%s.exe", binaryBaseName, osName, arch),
	}

	for _, candidate := range candidates {
		for _, asset := range rel.Assets {
			if strings.EqualFold(asset.Name, candidate) {
				copyAsset := asset
				return &copyAsset, nil
			}
		}
	}

	for _, asset := range rel.Assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, strings.ToLower(binaryBaseName)) &&
			strings.Contains(name, osName) &&
			strings.Contains(name, arch) &&
			!strings.HasSuffix(name, ".zip") &&
			!strings.HasSuffix(name, ".tar.gz") {
			copyAsset := asset
			return &copyAsset, nil
		}
	}

	return nil, fmt.Errorf("no matching asset found for %s on %s/%s", binaryBaseName, osName, arch)
}
