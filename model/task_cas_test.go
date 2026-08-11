package model

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&User{},
		&UserSession{},
		&AuthFlow{},
		&ExternalIdentityClaim{},
		&Token{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Log{},
		&Channel{},
		&QuotaData{},
		&Ability{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM auth_flows")
		DB.Exec("DELETE FROM external_identity_claims")
		DB.Exec("DELETE FROM user_sessions")
		DB.Exec("DELETE FROM passkey_credentials")
		DB.Exec("DELETE FROM two_fa_backup_codes")
		DB.Exec("DELETE FROM two_fas")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM user_oauth_bindings")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM perf_metrics")
		DB.Exec("DELETE FROM system_instances")
		DB.Exec("DELETE FROM system_task_locks")
		DB.Exec("DELETE FROM system_tasks")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}

func TestResetStuckMediaTasksBeforeOnlyResetsExpiredDownloads(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	stale := &Task{TaskID: "task_media_stale", MediaStatus: MediaStatusDownloading, MediaStartTime: now - 600, Data: json.RawMessage(`{}`)}
	fresh := &Task{TaskID: "task_media_fresh", MediaStatus: MediaStatusDownloading, MediaStartTime: now, Data: json.RawMessage(`{}`)}
	insertTask(t, stale)
	insertTask(t, fresh)

	require.NoError(t, ResetStuckMediaTasksBefore(now-300))

	var tasks []Task
	require.NoError(t, DB.Where("id IN ?", []int64{stale.ID, fresh.ID}).Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	assert.Equal(t, MediaStatusPending, tasks[0].MediaStatus)
	assert.Zero(t, tasks[0].MediaStartTime)
	assert.Equal(t, MediaStatusDownloading, tasks[1].MediaStatus)
}

func TestLegacyFailedMediaTaskCanBeScheduledForCompensation(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:      "task_legacy_media_failure",
		Status:      TaskStatusSuccess,
		MediaStatus: MediaStatusFailed,
		FailReason:  "download failed with status 403 Forbidden",
		Data:        json.RawMessage(`{"metadata":{"url":"https://example.test/video.mp4"}}`),
	}
	insertTask(t, task)
	require.NoError(t, DB.Model(task).Update("media_next_retry_at", nil).Error)

	legacy := GetLegacyFailedMediaTasks(10)
	require.Len(t, legacy, 1)
	assert.True(t, legacy[0].EnableLegacyMediaCompensation(now))
	assert.False(t, legacy[0].EnableLegacyMediaCompensation(now))

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, MediaStatusFailed, reloaded.MediaStatus)
	assert.Equal(t, now, reloaded.MediaNextRetryAt)
}

func TestQueueMediaCompensationUsesCASAndRespectsLimit(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_media_compensation",
		Status:           TaskStatusSuccess,
		MediaStatus:      MediaStatusFailed,
		MediaNextRetryAt: now,
		Data:             json.RawMessage(`{}`),
	}
	insertTask(t, task)

	first := *task
	second := *task
	assert.True(t, first.QueueMediaCompensation(now, now+300, 5))
	assert.False(t, second.QueueMediaCompensation(now, now+300, 5))

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, MediaStatusPending, reloaded.MediaStatus)
	assert.Equal(t, 1, reloaded.MediaRetryCount)
	assert.Equal(t, now+300, reloaded.MediaNextRetryAt)

	reloaded.MediaStatus = MediaStatusFailed
	reloaded.MediaRetryCount = 5
	reloaded.MediaNextRetryAt = now
	require.NoError(t, DB.Model(&reloaded).Updates(map[string]any{
		"media_status":        MediaStatusFailed,
		"media_retry_count":   5,
		"media_next_retry_at": now,
	}).Error)
	assert.False(t, reloaded.QueueMediaCompensation(now, now+300, 5))
}

func TestResetStuckMediaTasksFailsInterruptedImageTask(t *testing.T) {
	truncateTables(t)
	imageTask := &Task{
		TaskID:         "task_interrupted_image",
		Platform:       constant.TaskPlatformImage,
		Status:         TaskStatusInProgress,
		Progress:       "0%",
		MediaStatus:    MediaStatusDownloading,
		MediaStartTime: time.Now().Unix(),
		Data:           json.RawMessage(`{}`),
	}
	videoTask := &Task{
		TaskID:         "task_interrupted_video",
		Platform:       constant.TaskPlatform("55"),
		Status:         TaskStatusInProgress,
		Progress:       "0%",
		MediaStatus:    MediaStatusDownloading,
		MediaStartTime: time.Now().Unix(),
		Data:           json.RawMessage(`{}`),
	}
	insertTask(t, imageTask)
	insertTask(t, videoTask)

	ResetStuckMediaTasks()

	require.NoError(t, DB.First(imageTask, imageTask.ID).Error)
	require.NoError(t, DB.First(videoTask, videoTask.ID).Error)
	assert.Equal(t, TaskStatus(TaskStatusFailure), imageTask.Status)
	assert.Equal(t, "100%", imageTask.Progress)
	assert.Equal(t, MediaStatusFailed, imageTask.MediaStatus)
	assert.NotZero(t, imageTask.FinishTime)
	assert.NotEmpty(t, imageTask.FailReason)
	assert.Equal(t, MediaStatusPending, videoTask.MediaStatus)
}

func TestResetInvalidSoraMediaDownloads(t *testing.T) {
	truncateTables(t)
	invalid := &Task{
		TaskID:      "task_invalid_sora_json",
		Platform:    constant.TaskPlatform("55"),
		MediaStatus: MediaStatusSuccess,
		MediaURL:    "https://example.test/media/task.json",
		Data:        json.RawMessage(`{}`),
	}
	valid := &Task{
		TaskID:      "task_valid_sora_mp4",
		Platform:    constant.TaskPlatform("55"),
		MediaStatus: MediaStatusSuccess,
		MediaURL:    "https://example.test/media/task.mp4",
		Data:        json.RawMessage(`{}`),
	}
	insertTask(t, invalid)
	insertTask(t, valid)

	mediaURLs, err := ResetInvalidSoraMediaDownloads()
	require.NoError(t, err)
	assert.Equal(t, []string{invalid.MediaURL}, mediaURLs)

	var reloadedInvalid Task
	require.NoError(t, DB.First(&reloadedInvalid, invalid.ID).Error)
	assert.Equal(t, MediaStatusPending, reloadedInvalid.MediaStatus)
	assert.Empty(t, reloadedInvalid.MediaURL)
	var reloadedValid Task
	require.NoError(t, DB.First(&reloadedValid, valid.ID).Error)
	assert.Equal(t, MediaStatusSuccess, reloadedValid.MediaStatus)
	assert.Equal(t, valid.MediaURL, reloadedValid.MediaURL)
}
