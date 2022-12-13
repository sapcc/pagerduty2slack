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
	dryrun         bool
	syncOpts       *config.ScheduleSyncOptions
	err            error
	slackHandle    string
	pagerDutyIDs   []string
	pagerDutyUsers []pagerduty.User
	pagerDutyTeams []pagerduty.APIObject
	schedule       cron.Schedule
	pd             *pagerdutyclient.PDClient
	slackClient    *slackclient.SlackClient
}

func NewTeamSyncJob(cfg config.PagerdutyTeamToSlackGroup, dryrun bool, pd *pagerdutyclient.PDClient, slackClient *slackclient.SlackClient) (*PagerdutyTeamToSlackJob, error) {
	schedule, err := cron.ParseStandard(cfg.CrontabExpressionForRepetition)
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
		log.Warnf("User without Fon: %s %s", i2.Name, i2.HTMLURL)
	}

	// get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
	slackUserFilteredList, err := t.slackClient.MatchPDUsers(pdUsers)
	if err != nil {
		t.err = err
		return fmt.Errorf("job: sync of pd members for teams '%s' failed: %w", strings.Join(t.pagerDutyIDs, ","), err)
	}

	if len(slackUserFilteredList) == 0 && t.syncOpts.DisableSlackHandleTemporaryIfNoneOnShift {
		return t.slackClient.DisableGroup(t.slackHandle)
	}

	if _, err := t.slackClient.AddToGroup(t.slackHandle, slackUserFilteredList, t.dryrun); err != nil {
		return fmt.Errorf("job: updating slack group '%s' failed: %s", t.slackHandle, err.Error())
	}
	return nil
}

func (t *PagerdutyTeamToSlackJob) Name() string {
	return fmt.Sprintf("job: sync pagerduty team(s) '%s' to slack group: '%s'", strings.Join(t.pagerDutyIDs, ","), t.slackHandle)
}

func (t *PagerdutyTeamToSlackJob) JobType() string {
	return string(config.PdTeamSync)
}
func (t *PagerdutyTeamToSlackJob) PagerDutyObjects() []pagerduty.APIObject {
	return t.pagerDutyTeams
}

func (t *PagerdutyTeamToSlackJob) SlackHandleID() string {
	return t.slackHandle
}

func (t *PagerdutyTeamToSlackJob) Icon() string {
	return ":threepeople:"
}

func (t *PagerdutyTeamToSlackJob) Error() error {
	return t.err
}

func (t *PagerdutyTeamToSlackJob) Dryrun() bool {
	return t.dryrun
}

func (t *PagerdutyTeamToSlackJob) NextRun() time.Time {
	return t.schedule.Next(time.Now())
}

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
