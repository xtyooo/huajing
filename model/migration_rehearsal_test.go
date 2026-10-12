package model

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// This harness never invokes the application entry point or any worker. External
// engines are opt-in, loopback-only, and restricted to disposable rehearsal DBs.
// Set TEST_REHEARSAL_MODE=upgrade only after creating the old schema with the
// archived baseline model.migrateDB. The default SQLite upgrade is representative
// schema coverage, not evidence that the entire previous release was exercised.
func TestDatabaseMigrationRehearsal(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			dsn := ""
			mode := os.Getenv("TEST_REHEARSAL_MODE")
			if mode == "" {
				mode = "fresh"
			}
			if mode == "guard" && engine == "sqlite" {
				t.Skip("SQLite has no separate 32-bit integer wallet schema")
			}
			var dialector gorm.Dialector
			dbType := common.DatabaseTypeSQLite
			switch engine {
			case "sqlite":
				dsn = filepath.Join(t.TempDir(), "rehearsal.db")
				dialector = sqlite.Open(dsn)
			case "mysql":
				dsn = os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not set; MySQL evidence absent")
				}
				config, err := sqlmysql.ParseDSN(dsn)
				require.True(t, err == nil, "invalid local MySQL fixture DSN")
				host, _, err := net.SplitHostPort(config.Addr)
				require.True(t, err == nil && rehearsalLoopback(host) && config.Net == "tcp" && strings.HasPrefix(config.DBName, "newapi_rehearsal_"), "requires loopback newapi_rehearsal_ database")
				dialector, dbType = mysqlMigrationDialector{Dialector: *mysql.Open(dsn).(*mysql.Dialector)}, common.DatabaseTypeMySQL
			case "postgres":
				dsn = os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not set; PostgreSQL evidence absent")
				}
				config, err := pgx.ParseConfig(dsn)
				require.True(t, err == nil, "invalid local PostgreSQL fixture DSN")
				require.True(t, rehearsalLoopback(config.Host) && strings.HasPrefix(config.Database, "newapi_rehearsal_"), "requires loopback newapi_rehearsal_ database")
				for _, fallback := range config.Fallbacks {
					require.True(t, rehearsalLoopback(fallback.Host), "remote PostgreSQL fallback is not permitted")
				}
				dialector, dbType = postgresMigrationDialector{Dialector: *postgres.Open(dsn).(*postgres.Dialector)}, common.DatabaseTypePostgreSQL
			}
			trace := &rehearsalDDLTrace{Interface: logger.Default.LogMode(logger.Silent)}
			db, err := gorm.Open(dialector, &gorm.Config{Logger: trace})
			require.True(t, err == nil, "cannot open local rehearsal database")
			sqlDB, err := db.DB()
			require.NoError(t, err)
			// MySQL options repair holds an advisory-lock connection while writing.
			sqlDB.SetMaxOpenConns(2)
			t.Cleanup(func() { _ = sqlDB.Close() })
			oldDB, oldLog := DB, LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
			DB, LOG_DB = db, db
			if engine != "sqlite" {
				logDSN := os.Getenv("TEST_MYSQL_LOG_DSN")
				if engine == "postgres" {
					logDSN = os.Getenv("TEST_POSTGRES_LOG_DSN")
				}
				if logDSN != "" {
					LOG_DB = openRehearsalLogDB(t, engine, logDSN, trace)
					t.Log("separate SQL log database enabled")
				}
			}
			common.RedisEnabled, common.BatchUpdateEnabled = false, false
			common.SetDatabaseTypes(dbType, dbType)
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = oldDB, oldLog
				common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
				common.SetDatabaseTypes(oldMainType, oldLogType)
				initCol()
			})
			t.Setenv("SKIP_64BIT_QUOTA_SCHEMA_CHECK", "false")
			t.Logf("engine=%s phase=%s", engine, mode)
			var version string
			if engine == "sqlite" {
				require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
			} else {
				require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
			}
			t.Logf("engine_version=%s", version)
			tables, err := db.Migrator().GetTables()
			require.NoError(t, err)
			if mode == "guard" {
				require.Empty(t, tables, "quota guard requires a fresh disposable database")
				require.NoError(t, db.Exec("CREATE TABLE users (id int PRIMARY KEY, quota int, used_quota int, aff_quota int, aff_history int)").Error)
				require.NoError(t, db.Exec("INSERT INTO users (id, quota, used_quota, aff_quota, aff_history) VALUES (8701,2147483647,22000,1200,2400)").Error)
				trace.ddl = nil
				require.ErrorContains(t, ensureUserQuotaColumns(db, dbType), "32-bit is not supported")
				assert.Empty(t, trace.ddl, "guard must reject before startup schema changes")
				var wallet struct {
					Quota      int
					UsedQuota  int
					AffQuota   int
					AffHistory int
				}
				require.NoError(t, db.Table("users").Select("quota,used_quota,aff_quota,aff_history").Where("id = ?", 8701).Scan(&wallet).Error)
				assert.Equal(t, 2147483647, wallet.Quota)
				assert.Equal(t, 22000, wallet.UsedQuota)
				assert.Equal(t, 1200, wallet.AffQuota)
				assert.Equal(t, 2400, wallet.AffHistory)
				return
			}
			if mode == "fresh" {
				require.Empty(t, tables, "fresh rehearsal requires an empty disposable database")
				if LOG_DB != DB {
					logTables, err := LOG_DB.Migrator().GetTables()
					require.NoError(t, err)
					require.Empty(t, logTables, "fresh split-log database must be empty")
				}
			} else {
				require.Equal(t, "upgrade", mode, "unsupported rehearsal mode")
				require.True(t, db.Migrator().HasTable("users"), "create archived baseline schema first")
				var count int64
				require.NoError(t, db.Table("users").Count(&count).Error)
				if count != 0 {
					require.Equal(t, int64(1), count, "only the explicit archived baseline fixture is allowed")
					var fixture struct {
						Id       int
						Username string
					}
					require.NoError(t, db.Table("users").Select("id", "username").First(&fixture).Error)
					require.Equal(t, 8600, fixture.Id)
					require.Equal(t, "baseline-fixture", fixture.Username)
				}
				// The old int declaration does not establish the physical column type.
				// If the real fixture is 32-bit, prove rejection before any startup DDL,
				// then widen the local fixture
				// explicitly; this is not an automatic production migration.
				if err := ensureUserQuotaColumns(db, dbType); err != nil {
					require.NotEqual(t, "sqlite", engine)
					for _, column := range userQuotaColumns {
						statement := "ALTER TABLE users ALTER COLUMN " + column + " TYPE bigint"
						if engine == "mysql" {
							statement = "ALTER TABLE users MODIFY COLUMN " + column + " bigint DEFAULT 0"
						}
						require.NoError(t, db.Exec(statement).Error)
					}
					t.Log("explicitly widened the four local historical wallet columns after guard rejection")
				}
				seedRehearsalHistoricalRows(t, db)
			}
			require.NoError(t, ensureUserQuotaColumns(db, dbType))
			require.NoError(t, migrateDB())
			require.NoError(t, migrateLOGDB())
			ledger := Log{Id: 9701, UserId: 8701, CreatedAt: 1700000000, Type: LogTypeConsume, Quota: 1400000, ModelName: "rehearsal-video", ChannelId: 8701, Other: `{"is_task":true}`}
			require.NoError(t, LOG_DB.Create(&ledger).Error)
			audit := AuditLog{EventId: "rehearsal-audit-9701", UserId: 8701, CreatedAt: 1700000000, Category: AuditCategoryOperation, Action: "rehearsal.verify", Success: true}
			require.NoError(t, LOG_DB.Create(&audit).Error)
			duplicateAudit := audit
			duplicateAudit.Id = 0
			result := LOG_DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&duplicateAudit)
			require.NoError(t, result.Error)
			assert.Zero(t, result.RowsAffected)
			if mode == "fresh" {
				seedRehearsalHistoricalRows(t, db)
			}
			assertRehearsalHistoricalRows(t, db)
			var savedLog Log
			require.NoError(t, LOG_DB.First(&savedLog, 9701).Error)
			assert.Equal(t, 1400000, savedLog.Quota)
			var savedAudit AuditLog
			require.NoError(t, LOG_DB.Where("event_id = ?", audit.EventId).First(&savedAudit).Error)
			assert.Equal(t, 8701, savedAudit.UserId)
			logs, total, err := GetUserLogs(8701, LogTypeConsume, 1699999999, 1700000001, "rehearsal-video", "", 0, 10, "", "", "")
			require.NoError(t, err)
			assert.Equal(t, int64(1), total)
			require.Len(t, logs, 1)
			assert.Equal(t, 1400000, logs[0].Quota)
			audits, total, err := GetAuditLogs(AuditLogFilter{UserId: 8701}, 0, 10, common.RoleRootUser)
			require.NoError(t, err)
			assert.Equal(t, int64(1), total)
			require.Len(t, audits, 1)
			assert.Equal(t, audit.EventId, audits[0].EventId)
			var retireBefore Option
			require.NoError(t, db.Where(commonKeyCol+" = ?", legacyAccessTokenRetireAtKey).First(&retireBefore).Error)
			trace.ddl = nil
			require.NoError(t, ensureUserQuotaColumns(db, dbType))
			require.NoError(t, migrateDB())
			require.NoError(t, migrateLOGDB())
			assert.Empty(t, trace.ddl, "second migration must not recreate/alter existing schema")
			assertRehearsalHistoricalRows(t, db)
			var retireAfter Option
			require.NoError(t, db.Where(commonKeyCol+" = ?", legacyAccessTokenRetireAtKey).First(&retireAfter).Error)
			assert.Equal(t, retireBefore.Value, retireAfter.Value, "restart must not extend retired-token deadline")
			// Data rewind proves fixture readback; engine-native dump/restore and
			// old-binary compatibility must still be performed by the release lane.
			require.NoError(t, db.Model(&User{}).Where("id = ?", 8701).Update("quota", 123).Error)
			require.NoError(t, db.Model(&User{}).Where("id = ?", 8701).Update("quota", 2147483658).Error)
			assertRehearsalHistoricalRows(t, db)
		})
	}
}

func openRehearsalLogDB(t *testing.T, engine, dsn string, trace logger.Interface) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	if engine == "mysql" {
		config, err := sqlmysql.ParseDSN(dsn)
		require.True(t, err == nil, "invalid local MySQL log fixture DSN")
		host, _, err := net.SplitHostPort(config.Addr)
		require.True(t, err == nil && rehearsalLoopback(host) && config.Net == "tcp" && strings.HasPrefix(config.DBName, "newapi_rehearsal_"), "requires loopback rehearsal log database")
		dialector = mysqlMigrationDialector{Dialector: *mysql.Open(dsn).(*mysql.Dialector)}
	} else {
		config, err := pgx.ParseConfig(dsn)
		require.True(t, err == nil, "invalid local PostgreSQL log fixture DSN")
		require.True(t, rehearsalLoopback(config.Host) && strings.HasPrefix(config.Database, "newapi_rehearsal_"), "requires loopback rehearsal log database")
		for _, fallback := range config.Fallbacks {
			require.True(t, rehearsalLoopback(fallback.Host), "remote PostgreSQL log fallback is not permitted")
		}
		dialector = postgresMigrationDialector{Dialector: *postgres.Open(dsn).(*postgres.Dialector)}
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: trace})
	require.True(t, err == nil, "cannot open local rehearsal log database")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func rehearsalLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type rehearsalDDLTrace struct {
	logger.Interface
	ddl []string
}

func (l *rehearsalDDLTrace) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, _ := fc()
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(upper, "ALTER ") || strings.HasPrefix(upper, "DROP ") || strings.HasPrefix(upper, "CREATE TABLE ") {
		l.ddl = append(l.ddl, sql)
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

func seedRehearsalHistoricalRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Table("users").Create(map[string]any{"id": 8701, "username": "rehearsal-user", "password": "fixture-unused", "aff_code": "fixture8701", "quota": 2147483658, "used_quota": 22000, "aff_quota": 1200, "aff_history": 2400, "m": 12.5, "auth_version": 3}).Error)
	require.NoError(t, db.Table("channels").Create(map[string]any{"id": 8701, "type": 81, "name": "rehearsal-channel", "key": "fixture-key-unused", "status": 1, "models": "rehearsal-video"}).Error)
	require.NoError(t, db.Table("abilities").Create(map[string]any{"group": "default", "model": "rehearsal-video", "channel_id": 8701, "enabled": true}).Error)
	require.NoError(t, db.Table("tasks").Create(map[string]any{"id": 8701, "task_id": "rehearsal-public-task", "platform": "81", "user_id": 8701, "channel_id": 8701, "quota": 1400000, "status": "IN_PROGRESS", "progress": "99%", "media_url": "", "media_status": 2, "media_start_time": 1700000000, "media_finish_time": 0, "media_retry_count": 2, "media_next_retry_at": 1700000060, "properties": `{"origin_model_name":"rehearsal-video","upstream_model_name":"vendor-alias","extra":{"resolution":"720p","duration":10}}`, "private_data": `{"key":"fixture-selected-key","upstream_task_id":"fixture-private-task","billing_source":"wallet","billing_context":{"origin_model_name":"rehearsal-video","group_ratio":1,"image_size_pricing":true,"image_size_tier":"2k"}}`, "data": []byte("{}")}).Error)
	for _, row := range []Option{{Key: "resolution_price_setting.rehearsal-video", Value: `{"enabled":true,"setting":{"720p":0.28}}`}, {Key: "billing_setting.skip_seconds", Value: `{"rehearsal-fixed":true}`}} {
		require.NoError(t, db.Create(&row).Error)
	}
	for kind := 58; kind <= 81; kind++ {
		id := 8800 + kind
		modelName := fmt.Sprintf("rehearsal-type-%d", kind)
		require.NoError(t, db.Table("channels").Create(map[string]any{"id": id, "type": kind, "name": modelName, "key": "fixture-key-unused", "status": 1, "models": modelName}).Error)
		require.NoError(t, db.Table("abilities").Create(map[string]any{"group": "default", "model": modelName, "channel_id": id, "enabled": true}).Error)
		require.NoError(t, db.Table("tasks").Create(map[string]any{"id": id, "task_id": modelName, "platform": strconv.Itoa(kind), "user_id": 8701, "channel_id": id, "quota": 100, "status": "IN_PROGRESS", "progress": "0%", "properties": "{}", "private_data": "{}", "data": []byte("{}")}).Error)
	}
}

func assertRehearsalHistoricalRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	var user User
	require.NoError(t, db.First(&user, 8701).Error)
	assert.Equal(t, 2147483658, user.Quota)
	assert.Equal(t, 22000, user.UsedQuota)
	assert.Equal(t, 1200, user.AffQuota)
	assert.Equal(t, 2400, user.AffHistoryQuota)
	assert.Equal(t, 12.5, user.M)
	assert.Equal(t, int64(3), user.AuthVersion)
	var task Task
	require.NoError(t, db.First(&task, 8701).Error)
	assert.Equal(t, "81", string(task.Platform))
	assert.Equal(t, 1400000, task.Quota)
	assert.Equal(t, "99%", task.Progress)
	assert.Equal(t, 2, task.MediaRetryCount)
	assert.Equal(t, "fixture-selected-key", task.PrivateData.Key)
	assert.Equal(t, "fixture-private-task", task.PrivateData.UpstreamTaskID)
	require.NotNil(t, task.PrivateData.BillingContext)
	assert.True(t, task.PrivateData.BillingContext.ImageSizePricing)
	assert.Equal(t, "2k", task.PrivateData.BillingContext.ImageSizeTier)
	assert.Equal(t, "720p", task.Properties.Extra["resolution"])
	var channel Channel
	require.NoError(t, db.First(&channel, 8701).Error)
	assert.Equal(t, 81, channel.Type)
	var abilities int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 8701).Count(&abilities).Error)
	assert.Equal(t, int64(1), abilities)
	var option Option
	require.NoError(t, db.Where(commonKeyCol+" = ?", "resolution_price_setting.rehearsal-video").First(&option).Error)
	assert.JSONEq(t, `{"enabled":true,"setting":{"720p":0.28}}`, option.Value)
	for kind := 58; kind <= 81; kind++ {
		var channel Channel
		require.NoError(t, db.First(&channel, 8800+kind).Error)
		assert.Equal(t, kind, channel.Type)
		var task Task
		require.NoError(t, db.First(&task, 8800+kind).Error)
		assert.Equal(t, strconv.Itoa(kind), string(task.Platform))
		assert.Equal(t, 100, task.Quota)
	}
	var sentinelCount int64
	require.NoError(t, db.Model(&Channel{}).Where("type = ?", 82).Count(&sentinelCount).Error)
	assert.Zero(t, sentinelCount, "representative baseline contains no previously used Dummy=82 records")
}
