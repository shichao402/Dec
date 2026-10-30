package ssot

import (
	"errors"
	"fmt"
	"time"
)

// 服务端全局设置的读写（config/settings.json）。
//
// 只有一份，所有设备共用。写入走 Store.Update，与资产、设备表同一条
// 提交路径：这样"谁在什么时候把间隔改成多少"既有 commit 可查，
// 也能随冷备一起进 git 历史。

// ErrBadSettings 表示设置值不合规。
var ErrBadSettings = errors.New("bad settings")

// ReadSettings 读取当前设置。文件缺失时返回默认值。
//
// 缺文件是正常状态（这个文件是后加的），不能当错误：代理每次握手都要读它，
// 让它因为一个"文件还没生成过"而失败，会把所有设备挡在更新检查之外。
func ReadSettings(s *Store) (Settings, error) {
	out, err := ReadJSON[Settings](s, FileSettings)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Settings{
				SchemaVersion:          SchemaVersion,
				UpdateCheckIntervalSec: DefaultUpdateCheckIntervalSec,
			}, nil
		}
		return Settings{}, err
	}
	if out.SchemaVersion == 0 {
		out.SchemaVersion = SchemaVersion
	}
	// 读出来的值也夹一次：文件可能被人手改过，或来自一个边界不同的旧版本。
	// 这里不报错，只把越界值收敛到合法区间，保证调用方拿到的总能直接用。
	out.UpdateCheckIntervalSec = NormalizeUpdateCheckInterval(out.UpdateCheckIntervalSec)
	return out, nil
}

// SetSettings 写入设置。
//
// 显式校验而不是夹取：写入路径是人（或管理面）发起的，越界多半是输入错误，
// 悄悄改成边界值会让人以为"设置生效了"，实际生效的是另一个数。
func SetSettings(s *Store, in Settings, author Author) (Settings, error) {
	if in.UpdateCheckIntervalSec <= 0 {
		return Settings{}, fmt.Errorf("%w: 检查间隔必须大于 0 秒", ErrBadSettings)
	}
	if in.UpdateCheckIntervalSec < MinUpdateCheckIntervalSec || in.UpdateCheckIntervalSec > MaxUpdateCheckIntervalSec {
		return Settings{}, fmt.Errorf("%w: 检查间隔必须在 %d 到 %d 秒之间",
			ErrBadSettings, MinUpdateCheckIntervalSec, MaxUpdateCheckIntervalSec)
	}

	saved := in
	saved.SchemaVersion = SchemaVersion
	saved.UpdatedAt = time.Now().UTC()
	_, err := s.Update(author, func(tx *Tx) (string, error) {
		if err := tx.WriteJSON(FileSettings, saved); err != nil {
			return "", err
		}
		return fmt.Sprintf("更新自动更新检查间隔为 %d 秒", saved.UpdateCheckIntervalSec), nil
	})
	if err != nil {
		return Settings{}, err
	}
	return saved, nil
}
