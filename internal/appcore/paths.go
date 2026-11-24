package appcore

import (
	"path/filepath"
	"strings"
)

// ExportDirName 导出子目录名（与 agent 包一致）。
const ExportDirName = "exports"

// joinExportDir 拼接导出目录绝对路径。
func joinExportDir(base string) string {
	return filepath.Join(base, ExportDirName)
}

// exportPathAllowed 校验目标文件是否位于导出目录内（防 ../ 逃逸）。
func exportPathAllowed(base, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || base == "" {
		return false
	}
	absBase, err := filepath.Abs(joinExportDir(base))
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
