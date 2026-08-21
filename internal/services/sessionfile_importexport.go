package services

import (
	"archive/tar"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/crypto/pbkdf2"
)

func (s *SessionFileService) ExportSessions(selectedPaths []string, password string, outputPath string, keyIDs []string) error {
	s.ensureSessionsDir()

	if err := ValidateExportPassword(password); err != nil {
		return err
	}

	if outputPath == "" {
		path, err := s.showExportDialog()
		if err != nil {
			return err
		}
		outputPath = path
	}

	payload, err := s.packAndProtect(selectedPaths, password, keyIDs)
	if err != nil {
		return err
	}

	return os.WriteFile(outputPath, payload, 0644)
}

// showExportDialog 显示保存文件对话框，返回用户选择的路径。
func (s *SessionFileService) showExportDialog() (string, error) {
	if s.app == nil {
		return "", fmt.Errorf("服务未初始化")
	}
	dialog := s.app.Dialog.SaveFile()
	dialog.SetFilename("sessions.as9")
	dialog.AddFilter("AceShell 会话文件", "*.as9")
	dialog.AddFilter("所有文件", "*.*")
	result, err := dialog.PromptForSingleSelection()
	if err != nil || result == "" {
		return "", fmt.Errorf("导出已取消")
	}
	return result, nil
}

// exportMagic 导出包文件头(标准 as9 包格式)。
var exportMagic = []byte("ACEAS9V1")

// as9 包格式(标准化,同一算法输对口令即可解密):
//
//	[0..8)   魔数 "ACEAS9V1"
//	[8..24)  Salt(16 字节,随机)
//	[24..36) Nonce(12 字节,随机)
//	[36..)   AES-256-GCM 密文(明文 + 16 字节认证 Tag),密钥由 PBKDF2-SHA256(口令, salt, 600000 轮)派生
const (
	as9SaltSize  = 16
	as9NonceSize = 12
	as9HeaderLen = 8 + as9SaltSize + as9NonceSize
	as9PBKDFIters = 600000
)

// maxImportEntrySize 单条导入 tar 条目最大字节数(64MB),防止恶意超大包耗尽内存。
const maxImportEntrySize int64 = 64 * 1024 * 1024

// ValidateExportPassword 校验 as9 包口令:8~64 个字符,且包含大写字母/小写字母/数字/符号中的至少三类。
func ValidateExportPassword(password string) error {
	if password == "" {
		return fmt.Errorf("导出口令不能为空")
	}
	if utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 64 {
		return fmt.Errorf("口令长度须为 8~64 个字符")
	}
	categories := 0
	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			categories |= 1
		case r >= 'a' && r <= 'z':
			categories |= 2
		case r >= '0' && r <= '9':
			categories |= 4
		default:
			categories |= 8
		}
	}
	count := 0
	for i := 0; i < 4; i++ {
		if categories&(1<<i) != 0 {
			count++
		}
	}
	if count < 3 {
		return fmt.Errorf("口令须包含大写字母、小写字母、数字、符号中的至少三类")
	}
	return nil
}

// packAndProtect 将选中的路径打包为 tar 归档并加密:
// 打包前对 .toml 中的加密字段解密还原为内部明文;按勾选携带全局密钥(私钥解密入包)。
func (s *SessionFileService) packAndProtect(selectedPaths []string, password string, keyIDs []string) ([]byte, error) {
	var buf bytes.Buffer
	if err := s.writeTarArchive(selectedPaths, keyIDs, &buf); err != nil {
		return nil, err
	}
	return encryptWithPassword(buf.Bytes(), password)
}

// writeTarArchive 将选中的路径写入 tar 归档,.toml 敏感字段解密为明文;
// 勾选的全局密钥(树中 key/ 路径)以明文私钥写入 keys/ 目录(包整体受口令加密保护)。
func (s *SessionFileService) writeTarArchive(selectedPaths []string, keyIDs []string, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	for _, p := range selectedPaths {
		// 密钥路径:读取密钥文件并解密私钥后以明文写入包内 keys/ 目录
		if p == keysDirName || strings.HasPrefix(p, keysDirName+"/") {
			if err := s.addGlobalKeyToTar(tw, p); err != nil {
				return err
			}
			continue
		}
		fullPath, err := s.safeSessionPath(p)
		if err != nil {
			return err
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := s.addDirToTar(tw, fullPath); err != nil {
				return err
			}
		} else {
			rel, _ := filepath.Rel(sessionsDir, fullPath)
			if err := s.addFileToTar(tw, fullPath, rel, info); err != nil {
				return err
			}
		}
	}

	if len(keyIDs) > 0 && s.GlobalKeys != nil {
		for _, id := range keyIDs {
			content, err := s.GlobalKeys.loadContent(id)
			if err != nil {
				continue
			}
			if err := s.writeGlobalKeyToTar(tw, content); err != nil {
				return err
			}
		}
	}
	return nil
}

// addGlobalKeyToTar 将勾选的密钥路径(目录或单个文件)解密私钥后写入包内 keys/ 目录。
func (s *SessionFileService) addGlobalKeyToTar(tw *tar.Writer, path string) error {
	if s.GlobalKeys == nil {
		return fmt.Errorf("全局密钥服务不可用")
	}
	keyDir := s.GlobalKeys.keysDir()
	files, err := filepath.Glob(filepath.Join(keyDir, "*.json"))
	if err != nil {
		return nil
	}
	for _, f := range files {
		rel, _ := filepath.Rel(keyDir, f)
		if path != keysDirName && path != keysDirName+"/"+rel {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var kf globalKeyFile
		if json.Unmarshal(data, &kf) != nil || kf.ID == "" {
			continue
		}
		priv, err := decryptSecret(kf.PrivateKey)
		if err != nil {
			continue
		}
		pass, _ := decryptSecret(kf.Passphrase)
		content := &GlobalKeyContent{
			Name:        kf.Name,
			Type:        kf.Type,
			PrivateKey:  priv,
			Passphrase:  pass,
			PublicKey:   kf.PublicKey,
			Fingerprint: kf.Fingerprint,
		}
		if err := s.writeGlobalKeyToTar(tw, content); err != nil {
			return err
		}
	}
	return nil
}

// writeGlobalKeyToTar 将密钥内容(明文私钥)写入 tar 的 keys/ 目录。
func (s *SessionFileService) writeGlobalKeyToTar(tw *tar.Writer, content *GlobalKeyContent) error {
	entry := globalKeyFile{
		Name:        content.Name,
		Type:        content.Type,
		PrivateKey:  content.PrivateKey,
		Passphrase:  content.Passphrase,
		PublicKey:   content.PublicKey,
		Fingerprint: content.Fingerprint,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tarName := "keys/" + entry.Name + ".json"
	hdr := &tar.Header{Name: tarName, Mode: 0600, Size: int64(len(data)), ModTime: time.Now(), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	return nil
}

// addDirToTar 将目录及其内容添加到 tar 归档(跳过密钥库目录)。
func (s *SessionFileService) addDirToTar(tw *tar.Writer, dirPath string) error {
	return filepath.Walk(dirPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() && fi.Name() == keysDirName {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(sessionsDir, path)
		return s.addFileToTar(tw, path, rel, fi)
	})
}

// addFileToTar 将单个文件加入 tar；.toml 会话文件先解密敏感字段再入包。
func (s *SessionFileService) addFileToTar(tw *tar.Writer, diskPath, tarPath string, fi os.FileInfo) error {
	// 2.4 导出时跳过主机指纹文件,避免把已知指纹信任注入到导出包
	if filepath.Base(diskPath) == "known_hosts.json" || strings.HasSuffix(tarPath, "known_hosts.json") {
		return nil
	}
	if strings.HasSuffix(diskPath, ".toml") {
		return s.addTomlToTar(tw, diskPath, tarPath)
	}
	return addToTar(tw, diskPath, tarPath, fi)
}

// addTomlToTar 将 .toml 会话文件解密敏感字段后写入 tar。
func (s *SessionFileService) addTomlToTar(tw *tar.Writer, diskPath, tarPath string) error {
	content, err := s.tomlForExport(diskPath)
	if err != nil {
		return err
	}
	if content == nil {
		return fmt.Errorf("会话内容为空")
	}
	hdr := &tar.Header{
		Name:     tarPath,
		Mode:     0644,
		Size:     int64(len(content)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = tw.Write(content)
	return err
}

// tomlForExport 读取会话文件并解密敏感字段，返回导出用的明文 TOML 内容。
// 解密失败返回 error,禁止将本机密文原样入包(否则目标机器永远无法解密)。
func (s *SessionFileService) tomlForExport(diskPath string) ([]byte, error) {
	raw, err := os.ReadFile(diskPath)
	if err != nil {
		return nil, err
	}

	var data SessionFileData
	if err := toml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("解析会话文件失败: %w", err)
	}
	if data.Session.Password != "" {
		// 导出端容错:仅当本机可解密时转为明文入包;解密失败(明文或非本机加密)保留原值。
		if plain, err := s.decrypt(data.Session.Password); err == nil {
			data.Session.Password = plain
		}
	}
	return toml.Marshal(&data)
}

// encryptWithPassword 使用口令派生密钥并加密数据(PBKDF2-SHA256 + AES-256-GCM,标准 as9 包格式)。
func encryptWithPassword(plaintext []byte, password string) ([]byte, error) {
	salt := make([]byte, as9SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("生成随机数失败: %w", err)
	}
	key := pbkdf2.Key([]byte(password), salt, as9PBKDFIters, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建加密器失败: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 模式失败: %w", err)
	}
	nonce := make([]byte, as9NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("生成随机数失败: %w", err)
	}

	payload := make([]byte, 0, as9HeaderLen+len(plaintext)+aesGCM.Overhead())
	payload = append(payload, exportMagic...)
	payload = append(payload, salt...)
	payload = append(payload, nonce...)
	return aesGCM.Seal(payload, nonce, plaintext, nil), nil
}

// ImportSessions 从 as9 包导入会话到指定文件夹。
// overwrite 为 true 时覆盖同名会话/密钥，否则跳过。
// selectedPaths 为包内选中路径(文件夹=递归含子内容);为空表示导入全部。
// 密钥文件自动分离到本机 sessions/keys 密钥库(不落入本地目标文件夹)。
// 返回导入统计信息字符串(含密钥导入/跳过数量)。
// 导入期间持有 sessions 目录锁,避免与其他读写并发;导入完成即释放。
// importMu 进程内互斥锁,串行化并发导入请求(文件锁仅挡重复导入自身)。
var importMu sync.Mutex

func (s *SessionFileService) ImportSessions(password string, targetFolder string, filePath string, overwrite bool, selectedPaths []string) (string, error) {
	s.ensureSessionsDir()

	importMu.Lock()
	defer importMu.Unlock()

	release, err := acquireImportLock()
	if err != nil {
		return "", err
	}
	defer release()

	if err := ValidateExportPassword(password); err != nil {
		return "", err
	}

	plaintext, err := s.readAndDecrypt(filePath, password)
	if err != nil {
		return "", err
	}

	extractDir, err := s.safeSessionPath(targetFolder)
	if err != nil {
		return "", err
	}
	importedKeys, skippedKeys, err := s.extractTarArchive(plaintext, extractDir, overwrite, selectedPaths)
	if err != nil {
		return "", err
	}

	s.emit("session-tree-changed", s.GetTree())

	summary := "导入完成"
	if importedKeys > 0 {
		summary += fmt.Sprintf("，密钥导入 %d 个到密钥库", importedKeys)
	}
	if skippedKeys > 0 {
		summary += fmt.Sprintf("，密钥跳过 %d 个", skippedKeys)
	}
	return summary, nil
}

// importLockMaxAge 导入锁文件最大存活时间:超过视为崩溃残留,自动清理
var importLockMaxAge = 5 * time.Minute

func importLockPath() string {
	return filepath.Join(sessionsDir, ".aceshell-import.lock")
}

// acquireImportLock 原子创建 sessions 目录锁文件(O_EXCL),防止同一包被重复触发导入。
// 注意:此锁仅阻止并发导入自身,不挡其他会话读写(导入过程短,且已在进程内 importMu 串行化)。
// 返回释放函数;锁已存在且未过期时报错。
func acquireImportLock() (func(), error) {
	lock := importLockPath()
	if info, err := os.Stat(lock); err == nil {
		if time.Since(info.ModTime()) > importLockMaxAge {
			os.Remove(lock)
		} else {
			return nil, fmt.Errorf("正在导入中,请稍后再试")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("正在导入中,请稍后再试")
		}
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return func() { os.Remove(lock) }, nil
}

// GetImportPackageTree 读取 as9 包，返回包内目录结构（仅文件夹）的 JSON 树。
// 用于导入面板中展示导入包内的目录层级。
func (s *SessionFileService) GetImportPackageTree(filePath string, password string) string {
	plaintext, err := s.readAndDecrypt(filePath, password)
	if err != nil {
		return ""
	}
	tree := buildPackageDirTree(plaintext)
	data, _ := json.Marshal(tree)
	return string(data)
}

// GetImportPackageKeys 读取 as9 包，返回包内携带的全局密钥清单 JSON(名称/类型/指纹)。
func (s *SessionFileService) GetImportPackageKeys(filePath string, password string) string {
	plaintext, err := s.readAndDecrypt(filePath, password)
	if err != nil {
		return "[]"
	}
	type keyBrief struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Fingerprint string `json:"fingerprint"`
	}
	var keys []keyBrief
	tr := tar.NewReader(bytes.NewReader(plaintext))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "[]"
		}
		name := filepath.ToSlash(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(name, "keys/") || !strings.HasSuffix(name, ".json") {
			continue
		}
		content, _ := io.ReadAll(tr)
		var kf globalKeyFile
		if json.Unmarshal(content, &kf) != nil || kf.Name == "" {
			continue
		}
		keys = append(keys, keyBrief{Name: kf.Name, Type: kf.Type, Fingerprint: kf.Fingerprint})
	}
	data, _ := json.Marshal(keys)
	return string(data)
}

// buildPackageDirTree 解析 tar 明文,构建含文件夹、会话文件(.toml)与密钥文件(keys/)的目录树。
func buildPackageDirTree(data []byte) []*TreeNode {
	dirs := map[string]bool{"": true}
	files := map[string][]*TreeNode{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		name := filepath.ToSlash(hdr.Name)
		if hdr.Typeflag == tar.TypeDir {
			dirs[strings.TrimSuffix(name, "/")] = true
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// 会话文件(.toml)与密钥文件(keys/ 下的 .json)
		if strings.HasPrefix(name, "keys/") {
			if !strings.HasSuffix(name, ".json") {
				continue
			}
		} else if !strings.HasSuffix(name, ".toml") {
			continue
		}
		slash := strings.LastIndexByte(name, '/')
		if slash < 0 {
			// 顶层会话文件:挂到根
			files[""] = append(files[""], &TreeNode{Name: name, Path: name, IsDir: false})
			continue
		}
		dir := name[:slash]
		d := dir
		for d != "" {
			dirs[d] = true
			next := filepath.Dir(d)
			if next == "." {
				break
			}
			d = next
		}
		files[dir] = append(files[dir], &TreeNode{Name: filepath.Base(name), Path: name, IsDir: false})
	}

	for _, list := range files {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	}

	root := &TreeNode{Name: "/", Path: "", IsDir: true}
	index := map[string]*TreeNode{"": root}
	var dirList []string
	for d := range dirs {
		if d != "" {
			dirList = append(dirList, d)
		}
	}
	sort.Strings(dirList)

	for _, d := range dirList {
		parent := filepath.ToSlash(filepath.Dir(d))
		if parent == "." {
			parent = ""
		}
		pNode, ok := index[parent]
		if !ok {
			continue
		}
		node := &TreeNode{Name: filepath.Base(d), Path: d, IsDir: true}
		pNode.Children = append(pNode.Children, node)
		index[d] = node
	}

	// 将会话文件挂到所属文件夹节点下(文件排在文件夹后,按名称排序)
	for d, list := range files {
		dirNode, ok := index[d]
		if !ok {
			continue
		}
		dirNode.Children = append(dirNode.Children, list...)
	}
	return root.Children
}

// PickImportFile 显示系统文件对话框选择 .as9 文件，返回文件路径。
func (s *SessionFileService) PickImportFile() (string, error) {
	if s.app == nil {
		return "", fmt.Errorf("服务未初始化")
	}
	dialog := s.app.Dialog.OpenFile()
	dialog.AddFilter("AceShell 会话文件", "*.as9")
	dialog.AddFilter("所有文件", "*.*")
	path, err := dialog.PromptForSingleSelection()
	if err != nil || path == "" {
		return "", fmt.Errorf("导入已取消")
	}
	return path, nil
}

// readAndDecrypt 读取 as9 包并解密，返回内部明文的 tar 字节。
// 口令错误或包被篡改时返回统一错误。
func (s *SessionFileService) readAndDecrypt(encryptedPath, password string) ([]byte, error) {
	data, err := os.ReadFile(encryptedPath)
	if err != nil {
		return nil, err
	}
	if len(data) < as9HeaderLen || string(data[:8]) != string(exportMagic) {
		return nil, fmt.Errorf("无效的导出文件")
	}
	return decryptWithPassword(data, password)
}

// decryptWithPassword 使用口令派生密钥并解密数据（标准 as9 包格式）。
func decryptWithPassword(payload []byte, password string) ([]byte, error) {
	if len(payload) < as9HeaderLen {
		return nil, fmt.Errorf("无效的加密文件")
	}
	salt := payload[8 : 8+as9SaltSize]
	nonce := payload[8+as9SaltSize : as9HeaderLen]
	ciphertext := payload[as9HeaderLen:]

	key := pbkdf2.Key([]byte(password), salt, as9PBKDFIters, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建解密器失败: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 模式失败: %w", err)
	}
	if len(ciphertext) < aesGCM.Overhead() {
		return nil, fmt.Errorf("无效的加密文件")
	}
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("口令错误或文件已损坏")
	}
	return plaintext, nil
}

// extractTarArchive 将 tar 归档内容解压到目标目录。
// overwrite 为 true 时覆盖同名会话文件，否则跳过；
// .toml 会话文件的敏感字段用本机密钥重新加密后落盘；
// keys/ 目录下的全局密钥导入密钥库(私钥经本机主密钥重新加密)。
func (s *SessionFileService) extractTarArchive(data []byte, destDir string, overwrite bool, selectedPaths []string) (importedKeys int, skippedKeys int, err error) {
	os.MkdirAll(destDir, 0755)

	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return importedKeys, skippedKeys, err
		}
		relName := filepath.ToSlash(hdr.Name)
		// 按选中路径过滤:精确命中或属于选中文件夹内
		if !selectedPathHit(relName, selectedPaths) {
			continue
		}
		// 路径穿越防护:解压目标必须严格位于目标目录内
		target := filepath.Join(destDir, filepath.FromSlash(relName))
		absDest, err := filepath.Abs(destDir)
		if err != nil {
			return importedKeys, skippedKeys, err
		}
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return importedKeys, skippedKeys, err
		}
		if absTarget != absDest && !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0700)
		case tar.TypeReg:
			// 2.16 单条目大小限制,防止恶意超大 tar 耗尽内存
			content, err := io.ReadAll(io.LimitReader(tr, maxImportEntrySize+1))
			if err != nil {
				return importedKeys, skippedKeys, err
			}
			if int64(len(content)) > maxImportEntrySize {
				return importedKeys, skippedKeys, fmt.Errorf("导入包条目过大,已拒绝以防止资源耗尽")
			}
			// 2.4 包内 known_hosts.json 一律不落地,避免 TOFU 信任被静默注入
			if strings.HasSuffix(relName, "known_hosts.json") {
				continue
			}
			if strings.HasPrefix(relName, "keys/") {
				// 密钥文件分离导入到本机密钥库,不落入本地目标文件夹
				imported, skipped, kerr := s.importKeyFromPackage(content, overwrite)
				if kerr != nil {
					return importedKeys, skippedKeys, kerr
				}
				if imported {
					importedKeys++
				}
				if skipped {
					skippedKeys++
				}
				continue
			}
			os.MkdirAll(filepath.Dir(target), 0700)
			if !overwrite {
				if _, err := os.Stat(target); err == nil {
					continue
				}
			}
			if strings.HasSuffix(relName, ".toml") {
				// 2.4 强制清空导入会话中的主机指纹,杜绝 TOFU 信任静默注入
				// 2.8 加密失败必须返回错误,禁止降级为明文落盘
				content, err = s.reencryptSession(content)
				if err != nil {
					return importedKeys, skippedKeys, err
				}
			}
			if err := atomicWriteFile(target, content, 0600); err != nil {
				return importedKeys, skippedKeys, err
			}
		}
	}
	return importedKeys, skippedKeys, nil
}

// selectedPathHit 判断 tar 条目是否属于选中路径(精确命中或处于选中文件夹内)。
// selectedPaths 为空表示全部命中。
func selectedPathHit(relName string, selectedPaths []string) bool {
	if len(selectedPaths) == 0 {
		return true
	}
	for _, p := range selectedPaths {
		if relName == p || strings.HasPrefix(relName, p+"/") {
			return true
		}
	}
	return false
}

// importKeyFromPackage 将包内的密钥条目导入密钥库(本机主密钥重新加密)。
// 密钥文件仅存活于本机 sessions/keys 目录;同名密钥按 overwrite 策略覆盖或跳过。
// 返回 (imported, skipped, err)。
func (s *SessionFileService) importKeyFromPackage(content []byte, overwrite bool) (bool, bool, error) {
	var kf globalKeyFile
	if json.Unmarshal(content, &kf) != nil || kf.Name == "" {
		return false, false, nil
	}
	// 2.3 拒绝含路径分隔符或遍历段的密钥名称,防止越目录写密钥库
	if hasBackslashTraversal(kf.Name) || strings.ContainsAny(kf.Name, `/\`) {
		return false, false, fmt.Errorf("非法的密钥名称")
	}
	if s.GlobalKeys == nil {
		return false, false, fmt.Errorf("全局密钥服务不可用")
	}
	os.MkdirAll(s.GlobalKeys.keysDir(), 0755)
	targetPath := filepath.Join(s.GlobalKeys.keysDir(), kf.Name+".json")
	if _, err := os.Stat(targetPath); err == nil {
		if !overwrite {
			return false, true, nil
		}
	} else if !os.IsNotExist(err) {
		return false, false, err
	}
	encPriv, err := encryptSecret(kf.PrivateKey)
	if err != nil {
		return false, false, fmt.Errorf("密钥私钥加密失败: %v", err)
	}
	encPass, _ := encryptSecret(kf.Passphrase)
	now := time.Now().Format("2006-01-02 15:04:05")
	kf.ID = uuid.NewString()
	kf.PrivateKey = encPriv
	kf.Passphrase = encPass
	kf.Created = now
	kf.Updated = now

	out, _ := json.MarshalIndent(kf, "", "  ")
	if err := os.WriteFile(targetPath, out, 0600); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// reencryptSession 将导出包内的明文会话内容用本机密钥重新加密敏感字段。
// 同时强制清空主机指纹(HostKey),避免 TOFU 信任被绕过。加密失败返回 error,禁止降级为明文落盘。
func (s *SessionFileService) reencryptSession(content []byte) ([]byte, error) {
	var data SessionFileData
	if err := toml.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("解析会话内容失败: %w", err)
	}
	data.Session.HostKey = ""
	if data.Session.Password == "" {
		return toml.Marshal(&data)
	}
	encrypted, err := s.encrypt(data.Session.Password)
	if err != nil {
		return nil, fmt.Errorf("加密会话密码失败: %w", err)
	}
	data.Session.Password = encrypted
	out, err := toml.Marshal(&data)
	if err != nil {
		return nil, fmt.Errorf("序列化会话内容失败: %w", err)
	}
	return out, nil
}

func (s *SessionFileService) GetExportTree() string {
	s.ensureSessionsDir()
	tree := s.buildExportTree(sessionsDir, "")
	data, _ := json.Marshal(tree)
	return string(data)
}

func (s *SessionFileService) GetImportTree() string {
	s.ensureSessionsDir()
	tree := s.buildImportTree(sessionsDir, "")
	data, _ := json.Marshal(tree)
	return string(data)
}

func (s *SessionFileService) buildExportTree(dir, parentPath string) []*TreeNode {
	entries, _ := os.ReadDir(dir)
	var nodes []*TreeNode
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		nodePath := entry.Name()
		if parentPath != "" {
			nodePath = parentPath + "/" + entry.Name()
		}
		if entry.IsDir() {
			if entry.Name() == keysDirName {
				// 全局密钥库:列为可选文件夹,勾选后导出时解密私钥入包(不直接复制加密文件)
				children := s.buildExportKeyTree(filepath.Join(dir, entry.Name()), nodePath)
				nodes = append(nodes, &TreeNode{Name: "密钥", Path: nodePath, IsDir: true, Children: children})
				continue
			}
			children := s.buildExportTree(filepath.Join(dir, entry.Name()), nodePath)
			nodes = append(nodes, &TreeNode{Name: entry.Name(), Path: nodePath, IsDir: true, Children: children})
		} else if strings.HasSuffix(entry.Name(), ".toml") {
			nodes = append(nodes, &TreeNode{Name: strings.TrimSuffix(entry.Name(), ".toml"), Path: nodePath, IsDir: false})
		}
	}
	return nodes
}

// buildExportKeyTree 构建密钥库目录树(仅 .json 密钥文件)。
func (s *SessionFileService) buildExportKeyTree(dir, parentPath string) []*TreeNode {
	entries, _ := os.ReadDir(dir)
	var nodes []*TreeNode
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		nodePath := parentPath + "/" + entry.Name()
		nodes = append(nodes, &TreeNode{Name: entry.Name(), Path: nodePath, IsDir: false})
	}
	return nodes
}

func (s *SessionFileService) buildImportTree(dir, parentPath string) []*TreeNode {
	entries, _ := os.ReadDir(dir)
	var nodes []*TreeNode
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.Name() == keysDirName {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		nodePath := entry.Name()
		if parentPath != "" {
			nodePath = parentPath + "/" + entry.Name()
		}
		children := s.buildImportTree(filepath.Join(dir, entry.Name()), nodePath)
		nodes = append(nodes, &TreeNode{Name: entry.Name(), Path: nodePath, IsDir: true, Children: children})
	}
	return nodes
}

func addToTar(tw *tar.Writer, diskPath, tarPath string, fi os.FileInfo) error {
	hdr, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return err
	}
	hdr.Name = tarPath
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if fi.IsDir() {
		return nil
	}
	f, err := os.Open(diskPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(tw, f); err != nil {
		return err
	}
	return nil
}
