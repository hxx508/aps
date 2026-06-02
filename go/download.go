11111111
package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func checkAndUpdate() {
	logMsg("正在获取远程配置文件 version.txt ...")
	remoteVersions := getRemoteVersions(VersionUrl)
	localVersions := getLocalVersions(LocalVersionFile)

	isFirstRun := len(localVersions) == 0
	if isFirstRun {
		logMsg("首次运行，将下载所有软件包...")
	}

	updatedAny := false

	// 比对 nginx 上的所有包（不仅限于 filesToDownload）
	for filename, remoteVer := range remoteVersions {
		localVer := localVersions[filename]

		// 首次运行或版本不一致（包括新增的包）时下载
		if isFirstRun || localVer == "" || localVer != remoteVer {
			if isFirstRun || localVer == "" {
				logMsg(">>>> [" + filename + "] 新增/首次，下载版本 " + remoteVer)
			} else {
				logMsg(">>>> 检测到 [" + filename + "] 需要更新 (本地: " + localVer + ", 远程: " + remoteVer + ")")
			}

			destFile := filepath.Join(WorkDir, filename)
			if err := downloadFile(BaseUrl+filename, destFile); err != nil {
				logMsg("下载失败 " + filename + ": " + err.Error())
				continue
			}
			logMsg("下载成功 " + filename)

			if strings.HasSuffix(strings.ToLower(filename), ".zip") {
				if err := prepareExtractTarget(filename); err != nil {
					logMsg("解压准备失败 " + filename + ": " + err.Error())
					continue
				}

				logMsg("正在解压 " + filename + " ...")
				if err := unzip(destFile, WorkDir); err != nil {
					logMsg("解压失败 " + filename + ": " + err.Error())
					continue
				}
				logMsg("解压成功 " + filename)

				if err := waitAfterExtract(filename); err != nil {
					logMsg("解压后等待失败 " + filename + ": " + err.Error())
					continue
				}
			}

			localVersions[filename] = remoteVer
			updatedAny = true
			continue
		}

		logMsg("[" + filename + "] 已是最新版本 (" + localVer + ")，跳过下载")
	}

	if updatedAny {
		saveLocalVersions(LocalVersionFile, localVersions)
		logMsg("本地版本号文件 version.txt 已更新")
	}
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	extractedFiles := 0
	skippedFiles := 0

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			if canSkipExistingAPIMSFile(f.Name, fpath) {
				skippedFiles++
				logMsg(fmt.Sprintf("[提示] api-ms 文件已存在，跳过: %s", f.Name))
				continue
			}

			return fmt.Errorf("解压文件失败 %s: %w", f.Name, err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("打开压缩包内文件失败 %s: %w", f.Name, err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return fmt.Errorf("写入解压文件失败 %s: %w", f.Name, err)
		}

		extractedFiles++
	}

	if extractedFiles == 0 {
		return fmt.Errorf("未成功解压任何文件")
	}

	if skippedFiles > 0 {
		logMsg(fmt.Sprintf("[提示] 解压完成，但有 %d 个文件被跳过", skippedFiles))
	}

	if err := validateExtractedPackage(filepath.Base(src), dest); err != nil {
		return err
	}
	return nil
}

func canSkipExistingAPIMSFile(zipEntryName, targetPath string) bool {
	if !strings.HasPrefix(strings.ToLower(filepath.Base(zipEntryName)), "api-ms-win-core-") {
		return false
	}

	_, err := os.Stat(targetPath)
	return err == nil
}

func prepareExtractTarget(filename string) error {
	switch strings.ToLower(filename) {
	case "jdk17.zip":
		jdkDir := filepath.Join(WorkDir, "jdk17")
		if _, err := os.Stat(jdkDir); err == nil {
			logMsg("检测到旧 JDK 目录，准备清理: " + jdkDir)
			if err := os.RemoveAll(jdkDir); err != nil {
				return fmt.Errorf("清理旧 JDK 目录失败: %w", err)
			}
			logMsg("旧 JDK 目录已清理")
		}
	}

	return nil
}

func waitAfterExtract(filename string) error {
	switch strings.ToLower(filename) {
	case "jdk17.zip":
		logMsg("JDK 解压完成，等待文件句柄释放...")
		time.Sleep(10 * 60 * time.Second)
		logMsg("JDK 解压后等待完成")
	}

	return nil
}

func validateExtractedPackage(zipName, dest string) error {
	switch strings.ToLower(zipName) {
	case "jdk17.zip":
		javaw := filepath.Join(dest, "jdk17", "bin", "javaw.exe")
		if _, err := os.Stat(javaw); err != nil {
			return fmt.Errorf("JDK 解压不完整，缺少关键文件: %s", javaw)
		}
	case "tomcat.zip":
		bootstrapJar := filepath.Join(dest, "tomcat", "bin", "bootstrap.jar")
		if _, err := os.Stat(bootstrapJar); err != nil {
			return fmt.Errorf("Tomcat 解压不完整，缺少关键文件: %s", bootstrapJar)
		}
	}

	return nil
}

func getRemoteVersions(url string) map[string]string {
	versions := make(map[string]string)
	resp, err := http.Get(url)
	if err != nil {
		logMsg("获取远程 version.txt 失败: " + err.Error())
		return versions
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
			logMsg("解析远程 version.txt 失败: " + err.Error())
		}
	} else {
		logMsg(fmt.Sprintf("远程 version.txt 访问失败 (HTTP %d)", resp.StatusCode))
	}
	return versions
}

func getLocalVersions(path string) map[string]string {
	versions := make(map[string]string)
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &versions)
	}
	return versions
}

func saveLocalVersions(path string, versions map[string]string) {
	data, err := json.MarshalIndent(versions, "", "  ")
	if err == nil {
		os.WriteFile(path, data, 0644)
	}
}
