package config

type ObjectSyncType string

const (
	PdScheduleSync ObjectSyncType = "PD Schedule"
	PdTeamSync     ObjectSyncType = "PD Team"
)
