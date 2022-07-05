package main

import (
	"testing"

	"github.com/sapcc/pagerduty2slack/internal/config"
)

func TestAddMembersToGroups(t *testing.T) {

	jobInfo := config.JobInfo{JobType: config.PdTeamSync}

	addScheduleOnDutyMembersToGroups(jobInfo)

}
