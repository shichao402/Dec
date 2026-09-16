package app

import "context"

// migrateRemotePlanes 已废弃：官方远端不再由本机改写。
func migrateRemotePlanes(_ context.Context, _ Reporter) error {
	return nil
}
