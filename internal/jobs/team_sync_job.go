package jobs

import (
	"fmt"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"

	pagerdutyclient "github.com/sapcc/pagerduty2slack/internal/clients/pagerduty"
	slackclient "github.com/sapcc/pagerduty2slack/internal/clients/slack"
	"github.com/sapcc/pagerduty2slack/internal/config"
)

type PagerdutyTeamToSlackJob struct {
	syncOpts *config.ScheduleSyncOptions // options for tasks during sync
	schedule cron.Schedule               // on which this job runs
	dryrun   bool                        // when enabled changes are not manifested
	err      error                       // err used for slack info message

	pd          *pagerdutyclient.Client // pagerduty API access
	slackClient *slackclient.Client     // slack API access

	slackHandle string // of the target user group
	// TODO: get pagerdutyTeams when creating the Job?
	pagerDutyIDs   []string              // IDs of the team(s) to sync
	pagerDutyUsers []pagerduty.User      // users part of the pagerduty team(s)
	pagerDutyTeams []pagerduty.APIObject // pagerduty team(s) synced by this job
}

// NewSchedulesSyncJob creates a new job to sync members of pagerduty teams to a slack user group
func NewTeamSyncJob(cfg config.PagerdutyTeamToSlackGroup, dryrun bool, pd *pagerdutyclient.Client, slackClient *slackclient.Client) (*PagerdutyTeamToSlackJob, error) {
	schedule, err := cron.ParseStandard("TZ=UTC " + cfg.CrontabExpressionForRepetition)
	if err != nil {
		return nil, fmt.Errorf("job: invalid cron schedule '%s': %w", cfg.CrontabExpressionForRepetition, err)
	}
	return &PagerdutyTeamToSlackJob{
		schedule:     schedule,
		slackHandle:  cfg.ObjectsToSync.SlackGroupHandle,
		pagerDutyIDs: cfg.ObjectsToSync.PagerdutyObjectIDs,
		pd:           pd,
		slackClient:  slackClient,
		dryrun:       dryrun,
	}, nil
}

// Run syncs pagerduty team(s) members to slack user group
func (t *PagerdutyTeamToSlackJob) Run() error {
	log.Info(t.Name())

	// find members of given group
	pdUsers, pdTeams, err := t.pd.TeamMembers(t.pagerDutyIDs)
	if err != nil {
		t.err = err
		return fmt.Errorf("job: sync of pd members for teams '%s' failed: %w", strings.Join(t.pagerDutyIDs, ","), err)
	}
	t.pagerDutyTeams = pdTeams
	t.pagerDutyUsers = pdUsers

	pdUsersWithoutPhone := t.pd.WithoutPhone(pdUsers)
	for _, i2 := range pdUsersWithoutPhone {
		log.Infof("job: team_sync: pagerduty user without Fon: %s %s", i2.Name, i2.HTMLURL)
	}

	// get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
	slackUserFilteredList, err := t.slackClient.MatchPDUsers(pdUsers)
	if err != nil {
		t.err = err
		return fmt.Errorf("job: sync of pd members for teams '%s' failed: %w", strings.Join(t.pagerDutyIDs, ","), err)
	}

	if len(slackUserFilteredList) == 0 && t.syncOpts.DisableSlackHandleTemporaryIfNoneOnShift {
		if err := t.slackClient.DisableGroup(t.slackHandle); err != nil {
			t.err = err
			return err
		}
	}

	if _, err := t.slackClient.AddToGroup(t.slackHandle, slackUserFilteredList, t.dryrun); err != nil {
		t.err = err
		return fmt.Errorf("job: updating slack group '%s' failed: %s", t.slackHandle, err.Error())
	}
	return nil
}

// Name of the job
func (t *PagerdutyTeamToSlackJob) Name() string {
	return fmt.Sprintf("job: sync pagerduty team(s) '%s' to slack group: '%s'", strings.Join(t.pagerDutyIDs, ","), t.slackHandle)
}

// Icon returns name of icon to show in Slack messages
func (t *PagerdutyTeamToSlackJob) Icon() string {
	return ":threepeople:"
}

// JobType as string
func (t *PagerdutyTeamToSlackJob) JobType() string {
	return string(PdTeamSync)
}

// SlackHandle of the slack user group
func (t *PagerdutyTeamToSlackJob) SlackHandle() string {
	return t.slackHandle
}

// PagerDutyObjects returns the pagerduty team(s) synced
func (t *PagerdutyTeamToSlackJob) PagerDutyObjects() []pagerduty.APIObject {
	return t.pagerDutyTeams
}

// SlackInfoMessageBody returns TextBlock describing the number of users on shift
func (t *PagerdutyTeamToSlackJob) SlackInfoMessageBody() *slack.TextBlockObject {
	group, err := t.slackClient.GetSlackGroup(t.slackHandle)
	var userCount = 0
	if err == nil {
		userCount = group.UserCount
	}
	return &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf("*Member Count:*\n `%d` are in this Slack group", userCount),
		Emoji:    false,
		Verbatim: false,
	}
}

// Dryrun is true when the job is not performing changes
func (t *PagerdutyTeamToSlackJob) Dryrun() bool {
	return t.dryrun
}

// NextRun returns the time from now when the cron is next executed
func (t *PagerdutyTeamToSlackJob) NextRun() time.Time {
	return t.schedule.Next(time.Now())
}

// Error if any occurred during the sync
func (t *PagerdutyTeamToSlackJob) Error() error {
	return t.err
}
