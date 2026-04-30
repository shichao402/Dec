package freshness

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shichao402/Dec/pkg/repo"
)

// lockStaleAge 超过这个年龄的 lock 被视为前一任 crash 残留，允许清理后重新抢占。
const lockStaleAge = 10 * time.Minute

// lockFileName 是所有项目共享的 freshness 锁文件名。
// 放到 ~/.dec/local/ 下，保证同一 bare repo 同一时刻最多一个后台 fetch 在跑。
const lockFileName = "freshness.lock"

// freshnessLockPath 解析 lock 文件的绝对路径。
func freshnessLockPath() (string, error) {
	rootDir, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "local", lockFileName), nil
}

// acquireFreshnessLock 尝试以排它方式拿到 lock。
//
// 返回值：
//   - (release, nil)：拿到了，调用方必须 defer release() 释放
//   - (nil, nil)：另一进程持有且未过期，调用方应直接 skip
//   - (nil, err)：路径解析 / 文件系统错误，调用方视作 skip
//
// 实现要点：
//  1. 拿之前看一眼既有 lock 的 mtime；>10min 视为僵尸 → 删掉
//  2. O_CREATE|O_EXCL 独占创建；已存在返回 IsExist → 返回 (nil, nil)
//  3. 写 pid 作为调试线索；即使写失败也算拿到了锁
//  4. 释放函数只做 os.Remove，失败沉默（反正下一次僵尸清理会兜底）
func acquireFreshnessLock() (func(), error) {
	path, err := freshnessLockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	// 僵尸 lock 清理：避免前一次子进程 crash 后永久卡住后续 fetch。
	if info, statErr := os.Stat(path); statErr == nil {
		if time.Since(info.ModTime()) > lockStaleAge {
			_ = os.Remove(path)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// 写 pid 方便排查卡死子进程；写不进去不致命。
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Close()

	release := func() {
		_ = os.Remove(path)
	}
	return release, nil
}
