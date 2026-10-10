// Package storage 存储层公共抽象：SQL 方言无关的执行接口与驱动分发
package storage

import (
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/postgres"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
)

// Open 按驱动打开数据库并执行迁移（driver: sqlite | postgres）
func Open(driver, dsn string) (dbexec.SQLDB, error) {
	switch driver {
	case "", "sqlite":
		return sqlite.Open(dsn)
	case "postgres", "postgresql", "pg":
		return postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("未知存储驱动: %s（支持 sqlite / postgres）", driver)
	}
}
