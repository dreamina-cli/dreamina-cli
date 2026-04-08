package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"code.byted.org/videocut-aigc/dreamina_cli/buildinfo"
)

const (
	versionFileName     = "version.json"
	versionCacheDirName = ".dreamina_cli"
	versionBaseURL      = "https://lf3-static.bytednsdoc.com/obj/eden-cn/psj_hupthlyk/ljhwZthlaukjlkulzlp"
)

var (
	updateResultChan chan UpdateResult
)

type VersionInfo struct {
	Version      string `json:"version"`
	ReleaseNotes string `json:"release_notes"`
	ReleaseDate  string `json:"release_date"`
}

type UpdateResult struct {
	HasUpdate      bool
	RemoteVersion  VersionInfo
	CurrentVersion string
	ErrorMessage   string
}

func init() {
	updateResultChan = make(chan UpdateResult, 1)
}

// CheckUpdateAsync 异步探测是否有更新提示。
func CheckUpdateAsync() {
	go func() {
		result, ok := checkUpdate(context.Background())
		if !ok {
			return
		}
		select {
		case updateResultChan <- result:
		default:
		}
	}()
}

// PrintUpdateResult 在命令结束后输出更新提示；若没有结果则直接返回。
func PrintUpdateResult() {
	select {
	case result := <-updateResultChan:
		printUpdateResult(result)
	default:
		return
	}
}

func printUpdateResult(result UpdateResult) {
	out := os.Stdout
	if strings.TrimSpace(result.ErrorMessage) != "" {
		_, _ = fmt.Fprintln(out, result.ErrorMessage)
		_ = touchLocalVersionFile()
		return
	}
	if result.HasUpdate {
		_, _ = fmt.Fprintf(out, "[Update Available] A new version %s is available (current: %s).\n", result.RemoteVersion.Version, result.CurrentVersion)
		if strings.TrimSpace(result.RemoteVersion.ReleaseNotes) != "" {
			_, _ = fmt.Fprintf(out, "Release Notes:\n%s\n", strings.TrimSpace(result.RemoteVersion.ReleaseNotes))
		}
		_ = touchLocalVersionFile()
		return
	}
	_, _ = fmt.Fprintf(out, "local [%v] is the latest version, pass\n", result.CurrentVersion)
	_ = touchLocalVersionFile()
}

func touchLocalVersionFile() error {
	path := getLocalVersionFilePath()
	now := time.Now()
	return os.Chtimes(path, now, now)
}

func checkUpdate(ctx context.Context) (UpdateResult, bool) {
	currentVersion := strings.TrimSpace(buildinfo.Version)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, localPath, err := getLocalVersion(ctx)
	if err != nil {
		return UpdateResult{
			CurrentVersion: currentVersion,
			ErrorMessage:   err.Error(),
		}, true
	}

	if localPath != "" {
		if info, statErr := os.Stat(localPath); statErr == nil {
			if time.Since(info.ModTime()) < time.Hour {
				return UpdateResult{}, false
			}
		}
	}

	remoteVersion, ok, err := fetchLatestVersionFromCDN(ctx)
	if err != nil {
		return UpdateResult{
			CurrentVersion: currentVersion,
			ErrorMessage:   err.Error(),
		}, true
	}
	if !ok {
		return UpdateResult{}, false
	}

	hasUpdate := isRemoteNewer(remoteVersion.Version, currentVersion)
	return UpdateResult{
		HasUpdate:      hasUpdate,
		RemoteVersion:  remoteVersion,
		CurrentVersion: currentVersion,
	}, true
}

func isRemoteNewer(remoteVersion string, currentVersion string) bool {
	remoteVersion = strings.TrimSpace(remoteVersion)
	currentVersion = strings.TrimSpace(currentVersion)
	if remoteVersion == "" || currentVersion == "" {
		return false
	}
	if remoteVersion == currentVersion {
		return false
	}
	remoteSemver, okRemote := parseSemver(remoteVersion)
	currentSemver, okCurrent := parseSemver(currentVersion)
	if okRemote && okCurrent {
		for i := 0; i < 3; i++ {
			if remoteSemver[i] > currentSemver[i] {
				return true
			}
			if remoteSemver[i] < currentSemver[i] {
				return false
			}
		}
		return false
	}
	return true
}

func parseSemver(value string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) < 3 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			return out, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func getLocalVersion(ctx context.Context) (VersionInfo, string, error) {
	path := getLocalVersionFilePath()
	if data, err := os.ReadFile(path); err == nil {
		var info VersionInfo
		if jsonErr := json.Unmarshal(data, &info); jsonErr == nil {
			return info, path, nil
		}
	}
	info, err := fetchLatestVersionFromCDNForCache(ctx)
	if err != nil {
		return VersionInfo{}, path, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return VersionInfo{}, path, fmt.Errorf("创建目录失败，目录路径为 %s，错误信息: %v，请重新执行curl命令更新", dir, err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		return VersionInfo{}, path, fmt.Errorf("写入 version.json 失败: %v，请重新执行curl命令更新", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return VersionInfo{}, path, fmt.Errorf("写入 version.json 失败: %v，请重新执行curl命令更新", err)
	}
	return info, path, nil
}

func fetchLatestVersionFromCDN(ctx context.Context) (VersionInfo, bool, error) {
	ok, err := checkCDNDirectory(ctx)
	if err != nil {
		return VersionInfo{}, false, err
	}
	if !ok {
		return VersionInfo{}, false, nil
	}
	body, err := downloadJSONFromCDN(ctx)
	if err != nil {
		return VersionInfo{}, false, err
	}
	var info VersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return VersionInfo{}, false, fmt.Errorf("parse remote version.json failed: %w", err)
	}
	return info, true, nil
}

func fetchLatestVersionFromCDNForCache(ctx context.Context) (VersionInfo, error) {
	body, err := downloadJSONFromCDN(ctx)
	if err != nil {
		return VersionInfo{}, fmt.Errorf("从 CDN 下载 version.json 失败: %v，请重新执行curl命令更新", err)
	}
	var info VersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return VersionInfo{}, fmt.Errorf("从 CDN 下载 version.json 失败: %v，请重新执行curl命令更新", err)
	}
	return info, nil
}

func downloadJSONFromCDN(ctx context.Context) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", versionBaseURL, versionFileName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func checkCDNDirectory(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionBaseURL, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	return bytes.Index(body, []byte(versionFileName)) != -1, nil
}

func getLocalVersionFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return versionFileName
	}
	return filepath.Join(home, versionCacheDirName, versionFileName)
}
