package inventory

import (
	"context"
	rawsql "database/sql"
	stdsql "database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/Mxucc/StorFlow/ent"
	_ "github.com/Mxucc/StorFlow/ent/runtime"
	"github.com/Mxucc/StorFlow/inventory/debug"
	"github.com/Mxucc/StorFlow/pkg/cache"
	"github.com/Mxucc/StorFlow/pkg/conf"
	"github.com/Mxucc/StorFlow/pkg/logging"
	"github.com/Mxucc/StorFlow/pkg/util"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"modernc.org/sqlite"
)

const (
	DBVersionPrefix           = "db_version_"
	EnvDefaultOverwritePrefix = "CR_SETTING_DEFAULT_"
	EnvEnableAria2            = "CR_ENABLE_ARIA2"
)

// InitializeDBClient runs migration and returns a new ent.Client with additional configurations
// for hooks and interceptors.
func InitializeDBClient(l logging.Logger,
	client *ent.Client, kv cache.Driver, requiredDbVersion string) (*ent.Client, error) {
	ctx := context.WithValue(context.Background(), logging.LoggerCtx{}, l)
	// Always run the idempotent schema sync first: custom builds iterate on
	// schemas without bumping the DB version marker, and this keeps new
	// tables/columns in sync on every startup.
	if err := client.Schema.Create(ctx); err != nil {
		return nil, fmt.Errorf("failed to sync database schema: %w", err)
	}
	if needMigration(client, ctx, requiredDbVersion) {
		// Run the auto migration tool.
		if err := migrate(l, client, ctx, kv, requiredDbVersion); err != nil {
			return nil, fmt.Errorf("failed to migrate database: %w", err)
		}
	} else {
		l.Info("Database schema is up to date.")
	}
	// Seed settings introduced by this build on installations that already carry
	// the current DB version marker (no migration run above).
	ensureDefaultSettings(l, client, ctx, kv)

	// 把仍然带着旧品牌痕迹的设置（发件人名称、法务链接、默认主题、
	// 内置 OAuth 客户端图标）升级成 StorFlow。只在上游默认值上生效，可重复执行。
	upgradeBrandingDefaults(l, client, ctx, kv)

	//createMockData(client, ctx)
	return client, nil
}

// NewRawEntClient returns a new ent.Client without additional configurations.
func NewRawEntClient(l logging.Logger, config conf.ConfigProvider) (*ent.Client, error) {
	client, _, _, err := NewRawEntClientWithDB(l, config)
	return client, err
}

// NewRawEntClientWithDB is NewRawEntClient plus the underlying database handle
// and the resolved dialect. Modules that own their own tables (the shop module)
// use the handle to run their own statements on the very same connection pool.
func NewRawEntClientWithDB(l logging.Logger, config conf.ConfigProvider) (*ent.Client, *stdsql.DB, DBType, error) {
	l.Info("Initializing database connection...")
	dbConfig := config.Database()
	confDBType := dbConfig.Type
	if confDBType == conf.SQLite3DB || confDBType == "" {
		confDBType = conf.SQLiteDB
	}
	if confDBType == conf.MariaDB {
		confDBType = conf.MySqlDB
	}

	var (
		err    error
		client *sql.Driver
	)

	// Check if the database type is supported.
	if confDBType != conf.SQLiteDB && confDBType != conf.MySqlDB && confDBType != conf.PostgresDB {
		return nil, nil, "", fmt.Errorf("unsupported database type: %s", confDBType)
	}
	// If Database connection string provided, use it directly.
	if dbConfig.DatabaseURL != "" {
		l.Info("Connect to database with connection string")
		client, err = sql.Open(string(confDBType), dbConfig.DatabaseURL)
	} else {

		switch confDBType {
		case conf.SQLiteDB:
			dbFile := util.RelativePath(dbConfig.DBFile)
			renameLegacyDBFile(l, dbFile)
			l.Info("Connect to SQLite database %q.", dbFile)
			client, err = sql.Open("sqlite3", dbFile)
		case conf.PostgresDB:
			l.Info("Connect to Postgres database %q.", dbConfig.Host)
			client, err = sql.Open("postgres", fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable",
				dbConfig.Host,
				dbConfig.User,
				dbConfig.Password,
				dbConfig.Name,
				dbConfig.Port))
		case conf.MySqlDB, conf.MsSqlDB:
			l.Info("Connect to MySQL/SQLServer database %q.", dbConfig.Host)
			var host string
			if dbConfig.UnixSocket {
				host = fmt.Sprintf("unix(%s)",
					dbConfig.Host)
			} else {
				host = fmt.Sprintf("(%s:%d)",
					dbConfig.Host,
					dbConfig.Port)
			}

			client, err = sql.Open(string(confDBType), fmt.Sprintf("%s:%s@%s/%s?charset=%s&parseTime=True&loc=Local",
				dbConfig.User,
				dbConfig.Password,
				host,
				dbConfig.Name,
				dbConfig.Charset))
		default:
			return nil, nil, "", fmt.Errorf("unsupported database type %q", confDBType)
		}

		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to open database: %w", err)
		}

	}
	// Set connection pool
	db := client.DB()
	db.SetMaxIdleConns(50)
	if confDBType == "sqlite" || confDBType == "UNSET" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(100)
	}

	// Set timeout
	db.SetConnMaxLifetime(time.Second * 30)

	driverOpt := ent.Driver(client)

	// Enable verbose logging for debug mode.
	if config.System().Debug {
		l.Debug("Debug mode is enabled for DB client.")
		driverOpt = ent.Driver(debug.DebugWithContext(client, func(ctx context.Context, i ...any) {
			logging.FromContext(ctx).Debug(i[0].(string), i[1:]...)
		}))
	}

	return ent.NewClient(driverOpt), db, DBType(confDBType), nil
}

// DBType is the normalized database dialect name.
type DBType string

type sqlite3Driver struct {
	*sqlite.Driver
}

type sqlite3DriverConn interface {
	Exec(string, []driver.Value) (driver.Result, error)
}

func (d sqlite3Driver) Open(name string) (conn driver.Conn, err error) {
	conn, err = d.Driver.Open(name)
	if err != nil {
		return
	}
	_, err = conn.(sqlite3DriverConn).Exec("PRAGMA foreign_keys = ON;", nil)
	if err != nil {
		_ = conn.Close()
	}
	return
}

func init() {
	rawsql.Register("sqlite3", sqlite3Driver{Driver: &sqlite.Driver{}})
}

// renameLegacyDBFile 在打开 SQLite 数据库前，把旧版本遗留的数据文件名改成新名字。
// 仅当新文件名不存在、旧文件名存在时执行，避免覆盖已有数据。
func renameLegacyDBFile(l logging.Logger, dbFile string) {
	legacy := filepath.Join(filepath.Dir(dbFile), conf.LegacyDBFileName)
	if util.Exists(dbFile) || !util.Exists(legacy) {
		return
	}

	if err := os.Rename(legacy, dbFile); err != nil {
		l.Warning("Failed to rename legacy database file %q to %q: %s", legacy, dbFile, err)
		return
	}

	l.Info("Renamed legacy database file %q to %q.", legacy, dbFile)
}
