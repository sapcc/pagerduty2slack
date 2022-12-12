package manager

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	pagerdutyclient "github.com/sapcc/pagerduty2slack/internal/clients/pagerduty"
	slackclient "github.com/sapcc/pagerduty2slack/internal/clients/slack"
	"github.com/sapcc/pagerduty2slack/internal/config"
)

type Manager struct {
	pagerduty *pagerdutyclient.PDClient
	slack     *slackclient.SlackClient
}

func New(pd *pagerdutyclient.PDClient, slack *slackclient.SlackClient) *Manager {
	return &Manager{
		pagerduty: pd,
		slack:     slack,
	}
}

// func AddScheduleOnDutyMembersToGroups(cfg config.Config, mj config.PagerdutyScheduleOnDutyToSlackGroup, jobCounter int) {
func (m *Manager) AddScheduleOnDutyMembersToGroups(jI config.JobInfo) (config.JobInfo, error) {
	log.Info(jI.JobName())

	// find members of given group
	pd, err := pagerdutyclient.New(&jI.Cfg.Pagerduty)
	if err != nil {
		return config.JobInfo{}, fmt.Errorf("adding members to slack groups failed: %w", err)
	}

	tfF, err := time.ParseDuration(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameForward)
	if err != nil {
		tfF = time.Nanosecond * 0
		eS := fmt.Sprintf("Invalid duration given in job %d: %s", jI.JobCounter, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameForward)
		log.Warn(eS)
		jI.Error = fmt.Errorf(eS)
	}
	tfB, err := time.ParseDuration(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameBackward)
	if err != nil {
		tfB = time.Nanosecond * 0
		eS := fmt.Sprintf("Invalid duration given in job %d: %s. Use default 0. (%s)", jI.JobCounter, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameBackward, err)
		log.Warn(eS)
		jI.Error = fmt.Errorf(eS)
	}

	pdUsers, pdSchedules, err := pd.ListOnCallUsers(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].ObjectsToSync.PagerdutyObjectID, tfF, tfB, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.SyncStyle)
	jI.PdObjects = pdSchedules
	jI.PdObjectMember = pdUsers
	if err != nil {
		jI.Error = err
		return jI, nil
	}

	// get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
	jI.SlackGroupUser, err = m.slack.MatchPDUsers(pdUsers)
	if err != nil {
		jI.Error = err
		return jI, nil
	}

	// put ldap users which also have a slack account to our slack group (who's not in the ldap group is out)
	if _, err = m.slack.AddToGroup(&jI, jI.SlackGroupUser); err != nil {
		return config.JobInfo{}, fmt.Errorf("adding OnDuty members to slack group failed: %w", err)
	}
	return jI, nil
}

// func AddTeamMembersToGroups(cfg config.Config, mj config.PagerdutyTeamToSlackGroup, jobCounter int) {
func (m *Manager) AddTeamMembersToGroups(jI config.JobInfo) config.JobInfo {
	log.Info(jI.JobName())

	// find members of given group
	pd, err := pagerdutyclient.New(&jI.Cfg.Pagerduty)
	if err != nil {
		log.Error(fmt.Sprintf("PROGRAMMER FAIL > %s", err))
		jI.Error = err
		return jI
	}
	pdUsers, pdTeams, err := pd.TeamMembers(jI.PagerDutyIDs())
	jI.PdObjects = pdTeams
	jI.PdObjectMember = pdUsers
	if err != nil {
		jI.Error = err
		return jI
	}

	pdUsersWithoutPhone := pd.WithoutPhone(pdUsers)
	for _, i2 := range pdUsersWithoutPhone {
		log.Warnf("User without Fon: %s %s", i2.Name, i2.HTMLURL)
	}

	// get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
	slackUserFilteredList, err := m.slack.MatchPDUsers(pdUsers)
	if err != nil {
		jI.Error = err
		return jI
	}

	if len(slackUserFilteredList) == 0 && jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.DisableSlackHandleTemporaryIfNoneOnShift {
		m.slack.DisableGroup(&jI)
	} else {
		if _, err := m.slack.AddToGroup(&jI, slackUserFilteredList); err != nil {
			log.Warnf("updating slack user group failed: %s", err.Error())
			return jI
		}
	}
	return jI
}
