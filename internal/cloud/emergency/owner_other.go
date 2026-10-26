//go:build !unix

package emergency

import "os"

// checkOwner 在非 Unix 平台上不做归属检查。
//
// Windows 没有 uid 这种「文件归属于某个数值身份」的语义，
// ACL 表达的是另一套东西，硬套只会得出错误结论。应急命令本身
// 也只面向 Linux 服务器，这里保持空实现只是为了让跨平台编译通过。
func checkOwner(dir string, info os.FileInfo) error { return nil }
