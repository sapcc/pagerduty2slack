package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	yaml "gopkg.in/yaml.v2"
)

// Config we need
type Config struct {
	Slack          SlackConfig     `yaml:"slack"`
	Pagerduty      PagerdutyConfig `yaml:"pagerduty"`
	Global         GlobalConfig    `yaml:"global"`
	Jobs           JobsConfig      `yaml:"jobs"`
	ConfigFilePath string
}

// GlobalConfig Options passed via cmd line
type GlobalConfig struct {
	// loglevel
	LogLevel string `yaml:"logLevel"`

	// write
	Write bool `yaml:"write"`

	RecheckInterval time.Duration
	// if true all task run at start
	RunAtStart bool `yaml:"runAtStart"`
}

// JobsConfig Real Work Definition
type JobsConfig struct {
	ScheduleSync []PagerdutyScheduleOnDutyToSlackGroup `yaml:"pd-schedules-on-duty-to-slack-group"`
	TeamSync     []PagerdutyTeamToSlackGroup           `yaml:"pd-teams-to-slack-group"`
}

// SlackConfig Struct
type SlackConfig struct {
	// Token to authenticate
	BotSecurityToken  string `yaml:"securityTokenBot"`
	UserSecurityToken string `yaml:"securityTokenUser"`
	InfoChannel       string `yaml:"infoChannel"`
	Workspace         string `yaml:"workspaceForChatLinks"`
}

// PagerdutyConfig Struct
type PagerdutyConfig struct {
	// Token to authenticate
	AuthToken string `yaml:"authToken"`
	APIUser   string `yaml:"apiUser"`
}

// PagerdutyScheduleOnDutyToSlackGroup Struct
type PagerdutyScheduleOnDutyToSlackGroup struct {
	CrontabExpressionForRepetition string              `yaml:"crontabExpressionForRepetition"`
	DisableHandleIfNoneOnShift     bool                `yaml:"disableSlackHandleTemporaryIfNoneOnShift"`
	CheckUserContactForPhoneSet    bool                `yaml:"informUserIfContactPhoneNumberMissing"`
	SyncOptions                    ScheduleSyncOptions `yaml:"syncOptions"`
	ObjectsToSync                  SyncObjects         `yaml:"syncObjects"`
}

// ScheduleSyncOptions SyncOptions Struct
type ScheduleSyncOptions struct {
	HandoverTimeFrameForward                 string `yaml:"handoverTimeFrameForward"`
	HandoverTimeFrameBackward                string `yaml:"handoverTimeFrameBackward"`
	DisableSlackHandleTemporaryIfNoneOnShift bool   `yaml:"disableSlackHandleTemporaryIfNoneOnShift"`
	InformUserIfContactPhoneNumberMissing    bool   `yaml:"informUserIfContactPhoneNumberMissing"`
	//TakeTheLayersNotTheFinal bool `yaml:"scheduleLayerFinalOnly"`
	SyncStyle SyncStyle `yaml:"syncStyle"`
}

// SyncStyle Type of which Layer (or combination) is used
type SyncStyle string

const (
	FinalLayer           = "FinalLayer"
	OverridesOnlyIfThere = "OverridesOnlyIfThere"
	AllActiveLayers      = "AllActiveLayers"
)

// PagerdutyTeamToSlackGroup Struct
type PagerdutyTeamToSlackGroup struct {
	CrontabExpressionForRepetition string      `yaml:"crontabExpressionForRepetition"`
	CheckUserContactForPhoneSet    bool        `yaml:"informUserIfContactPhoneNumberMissing"`
	ObjectsToSync                  SyncObjects `yaml:"syncObjects"`
}

// SyncObjects Struct
type SyncObjects struct {
	SlackGroupHandle  string   `yaml:"slackGroupHandle"`
	PagerdutyObjectID []string `yaml:"pdObjectIds"`
}

// NewConfig reads the configuration from the given filePath.
func NewConfig(configFilePath string) (cfg Config, err error) {
	if configFilePath == "" {
		return cfg, errors.New("path to configuration file not provided")
	}

	cfgBytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return cfg, fmt.Errorf("read configuration file: %s", err.Error())
	}
	err = yaml.Unmarshal(cfgBytes, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse configuration: %s", err.Error())
	}
	return cfg, nil
}
