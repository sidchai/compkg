package alertstore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sidchai/compkg/pkg/alert"

	"gorm.io/gorm"
)

// AlertLog 告警日志表模型
type AlertLog struct {
	ID          int64     `gorm:"primaryKey;autoIncrement;column:id"`
	ServiceName string    `gorm:"type:varchar(64);not null;column:service_name;index:idx_service_level"`
	MetricName  string    `gorm:"type:varchar(128);not null;column:metric_name"`
	Level       int       `gorm:"type:tinyint;not null;column:level;index:idx_service_level"`
	Title       string    `gorm:"type:varchar(256);not null;column:title"`
	Message     string    `gorm:"type:text;column:message"`
	Value       string    `gorm:"type:varchar(128);not null;column:value"`
	Threshold   string    `gorm:"type:varchar(128);not null;column:threshold"`
	Tags        string    `gorm:"type:json;column:tags"`
	CreatedAt   time.Time `gorm:"autoCreateTime;column:created_at;index:idx_created_at"`
}

// TableName 表名
func (AlertLog) TableName() string {
	return "iot_alert_log"
}

// GormAlertStore 基于 gorm 的告警持久化实现
type GormAlertStore struct {
	db *gorm.DB
}

// NewGormStore 创建 gorm 告警存储
func NewGormStore(db *gorm.DB) *GormAlertStore {
	return &GormAlertStore{db: db}
}

// Save 保存告警事件到数据库
func (s *GormAlertStore) Save(event alert.AlertEvent) error {
	tagsJSON := ""
	if len(event.Tags) > 0 {
		data, err := json.Marshal(event.Tags)
		if err == nil {
			tagsJSON = string(data)
		}
	}

	log := &AlertLog{
		ServiceName: event.ServiceName,
		MetricName:  event.MetricName,
		Level:       int(event.Level),
		Title:       event.Title,
		Message:     event.Message,
		Value:       fmt.Sprintf("%v", event.Value),
		Threshold:   fmt.Sprintf("%v", event.Threshold),
		Tags:        tagsJSON,
		CreatedAt:   event.Timestamp,
	}
	return s.db.Create(log).Error
}

// AutoMigrate 自动建表/迁移
func (s *GormAlertStore) AutoMigrate() error {
	return s.db.AutoMigrate(&AlertLog{})
}

// QueryByService 按服务名查询告警历史
func (s *GormAlertStore) QueryByService(serviceName string, limit int) ([]AlertLog, error) {
	var logs []AlertLog
	err := s.db.Where("service_name = ?", serviceName).
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// QueryByLevel 按级别查询告警历史
func (s *GormAlertStore) QueryByLevel(level int, limit int) ([]AlertLog, error) {
	var logs []AlertLog
	err := s.db.Where("level = ?", level).
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// QueryByTimeRange 按时间范围查询告警历史
func (s *GormAlertStore) QueryByTimeRange(start, end time.Time, limit int) ([]AlertLog, error) {
	var logs []AlertLog
	err := s.db.Where("created_at BETWEEN ? AND ?", start, end).
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}
