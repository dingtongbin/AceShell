package services

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 本文件: 插件安装目录的原子替换、可执行文件锁等待、残留清理。
//
// 背景: 更新(GitHub 安装)、热加载(重载)、恢复捆绑三条路径都需要"用新目录换掉旧目录"。
// 原先各处自行 RemoveAll → Rename, 有两个真实故障:
//  1. Rename 失败时旧目录已经没了 → 插件彻底消失(且无法回滚);
//  2. Windows 上进程退出到 exe 文件锁释放存在延迟, 紧随 Kill 的改名会间歇性
//     报 "Access is denied", 表现为"装完就没了"或"卸载失败"。
//
// 统一收敛到 swapPluginDir: 旧目录先改名留档(可回滚), 新目录再改名就位,
// 最后异步删除留档。任何一步失败都能退回到替换前的状态。

const (
	// dirLockBudget 等待 exe 文件锁释放的预算。Windows 上 client.Kill() 返回后
	// 进程可能尚未完全退出(句柄回收异步), 3s 足以覆盖实测的最坏情况。
	dirLockBudget = 3 * time.Second
	dirLockPoll   = 50 * time.Millisecond
	// dirStaleMarker 旧版本留档后缀标记: <id>.old-<纳秒时间戳>
	dirStaleMarker = ".old-"
	// dirInstallPrefix 安装临时目录前缀(须以 '.' 开头: 不匹配 pluginIDRe, 不会被当作插件扫描)
	dirInstallPrefix = ".install-"
)

// swapPluginDir 用 srcDir 替换 PluginsDir()/<id>, 旧版本先留档、失败可回滚。
//
// 语义要说准: 这不是单系统调用的原子替换 —— 留档改名到新目录就位之间存在
// 一个 <id> 短暂不存在的窗口。它保证的是: (1) 任何一步失败都退回替换前的
// 状态; (2) 崩溃后磁盘上留有可辨认的 .old-* 痕迹; (3) 旧 exe 被锁时等待而非
// 半途而废。运行期不存在并发扫描, 这个窗口不构成实际问题。
//
// srcDir 就地消费; 成功后 srcDir 路径不再存在。
func swapPluginDir(id, srcDir string) error {
	if !pluginIDRe.MatchString(id) {
		return fmt.Errorf("非法插件 ID: %s", id)
	}
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return fmt.Errorf("待安装目录不可用 %s: %w", srcDir, err)
	}
	finalDir := filepath.Join(PluginsDir(), id)

	// 旧实例若仍在运行, 其 exe 处于锁定状态, 必须先等锁再改名。
	if _, err := os.Stat(finalDir); err == nil {
		if err := waitUnlocked(filepath.Join(finalDir, id+exeSuffix()), dirLockBudget); err != nil {
			return err
		}
	}

	stale := ""
	if _, err := os.Stat(finalDir); err == nil {
		stale = finalDir + dirStaleMarker + strconv.FormatInt(time.Now().UnixNano(), 10)
		if err := retryRename(finalDir, stale, dirLockBudget); err != nil {
			return fmt.Errorf("旧版本留档失败: %w", err)
		}
	}

	if err := retryRename(srcDir, finalDir, dirLockBudget); err != nil {
		// 回滚: 留档改回原名, 至少保住旧版本可用。
		if stale != "" {
			if rerr := os.Rename(stale, finalDir); rerr != nil {
				return fmt.Errorf("落盘失败且回滚失败(旧版本留档于 %s): %v / %w", stale, err, rerr)
			}
		}
		return fmt.Errorf("落盘失败: %w", err)
	}

	if stale != "" {
		// 锁定未完全释放时这里会失败, 不影响本次替换结果, 留给启动期清理。
		_ = os.RemoveAll(stale)
	}
	return nil
}

// retryRename 在预算内重试 Rename。Windows 上目录改名同样可能撞到占用,
// 单次失败不代表永久失败; Unix 上首轮即成功, 退化为一次调用。
// 源路径消失属于永久性错误, 立即返回, 不空转烧完预算。
func retryRename(src, dst string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		err := os.Rename(src, dst)
		if err == nil {
			return nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(dirLockPoll)
	}
}

// removeAllWithRetry 在预算内重试 RemoveAll(卸载路径用: exe 锁未释放时首轮会失败)。
func removeAllWithRetry(path string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		err := os.RemoveAll(path)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(dirLockPoll)
	}
}

// cleanupStalePluginDirs 清理插件根目录残留: 旧版本留档(<id>.old-*)与安装临时目录(.install-*)。
// 正常路径不会留下它们(替换成功即删); 仅进程崩溃或文件锁未及时释放时残留。
// 须在扫描插件之前调用 —— 残留目录名含 '.' 不匹配 pluginIDRe, 不会被误当作插件, 但会占磁盘。
func cleanupStalePluginDirs() (removed int) {
	entries, err := os.ReadDir(PluginsDir())
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() || !isStalePluginDirName(e.Name()) {
			continue
		}
		if err := removeAllWithRetry(filepath.Join(PluginsDir(), e.Name()), dirLockBudget); err == nil {
			removed++
		} else {
			// 清理失败说明磁盘上有锁残留, 值得进错误日志供排查
			CollectError("plugin-dir", "cleanup-stale:"+e.Name(), err)
		}
	}
	return removed
}

// isStalePluginDirName 判定目录名是否为残留(含 = 旧版本留档 / 安装临时目录)。
// 严格匹配形态, 避免误删合法插件目录 —— 插件 ID 只允许 [a-z0-9-], 不含 '.'。
func isStalePluginDirName(name string) bool {
	if strings.HasPrefix(name, dirInstallPrefix) {
		return true
	}
	i := strings.Index(name, dirStaleMarker)
	if i <= 0 {
		return false
	}
	id, stamp := name[:i], name[i+len(dirStaleMarker):]
	if !pluginIDRe.MatchString(id) || stamp == "" {
		return false
	}
	// 时间戳段必须是纯数字(防止把 foo.old-bar 这类名字当残留)
	for _, r := range stamp {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
