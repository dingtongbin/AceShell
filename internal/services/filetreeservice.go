package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileTreeService struct{}

type FileTreeNode struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	IsDir    bool            `json:"isDir"`
	Children []*FileTreeNode `json:"children,omitempty"`
}

// GetScriptTree 返回脚本目录的树形结构 JSON。
func (f *FileTreeService) GetScriptTree() string {
	return f.getTree(ScriptsDir())
}

// GetAutoLogTree 返回自动日志目录的树形结构 JSON。
func (f *FileTreeService) GetAutoLogTree() string {
	return f.getTree(AutoLogDir())
}

func (f *FileTreeService) getTree(rootDir string) string {
	os.MkdirAll(rootDir, 0755)
	tree := f.buildTree(rootDir, "")
	data, _ := json.Marshal(tree)
	return string(data)
}

func (f *FileTreeService) buildTree(dir, parentPath string) []*FileTreeNode {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var dirs []*FileTreeNode
	var files []*FileTreeNode

	for _, entry := range entries {
		nodePath := entry.Name()
		if parentPath != "" {
			nodePath = parentPath + "/" + entry.Name()
		}

		if entry.IsDir() {
			children := f.buildTree(filepath.Join(dir, entry.Name()), nodePath)
			dirs = append(dirs, &FileTreeNode{
				Name:     entry.Name(),
				Path:     nodePath,
				IsDir:    true,
				Children: children,
			})
		} else {
			files = append(files, &FileTreeNode{
				Name:  entry.Name(),
				Path:  nodePath,
				IsDir: false,
			})
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	result := append(dirs, files...)
	return result
}

// CreateScriptFolder 在脚本目录下创建文件夹。
func (f *FileTreeService) CreateScriptFolder(folderPath string) error {
	return f.createFolder(ScriptsDir(), folderPath)
}

// CreateAutoLogFolder 在自动日志目录下创建文件夹。
func (f *FileTreeService) CreateAutoLogFolder(folderPath string) error {
	return f.createFolder(AutoLogDir(), folderPath)
}

func (f *FileTreeService) createFolder(root, folderPath string) error {
	fullPath, err := safeScriptPath(root, folderPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, 0755)
}

// DeleteScriptItem 删除脚本目录下的文件或文件夹。
func (f *FileTreeService) DeleteScriptItem(filePath string) error {
	return f.deleteItem(ScriptsDir(), filePath)
}

// DeleteAutoLogItem 删除自动日志目录下的文件或文件夹。
func (f *FileTreeService) DeleteAutoLogItem(filePath string) error {
	return f.deleteItem(AutoLogDir(), filePath)
}

func (f *FileTreeService) deleteItem(root, filePath string) error {
	fullPath, err := safeScriptPath(root, filePath)
	if err != nil {
		return err
	}
	return os.RemoveAll(fullPath)
}

// MoveScriptItem 移动脚本目录下的文件或文件夹到目标目录。
func (f *FileTreeService) MoveScriptItem(filePath, destFolder string) error {
	return f.moveItem(ScriptsDir(), filePath, destFolder)
}

// MoveAutoLogItem 移动自动日志目录下的文件或文件夹到目标目录。
func (f *FileTreeService) MoveAutoLogItem(filePath, destFolder string) error {
	return f.moveItem(AutoLogDir(), filePath, destFolder)
}

func (f *FileTreeService) moveItem(root, filePath, destFolder string) error {
	oldPath, err := safeScriptPath(root, filePath)
	if err != nil {
		return err
	}
	newDir, err := safeScriptPath(root, destFolder)
	if err != nil {
		return err
	}

	absOld, _ := filepath.Abs(oldPath)
	absNew, _ := filepath.Abs(newDir)
	if strings.HasPrefix(absNew, absOld+string(os.PathSeparator)) {
		return fmt.Errorf("不能移动到自身内部")
	}

	os.MkdirAll(newDir, 0755)
	newPath := filepath.Join(newDir, filepath.Base(filePath))
	return os.Rename(oldPath, newPath)
}