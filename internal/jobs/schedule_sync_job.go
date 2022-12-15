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

type PagerdutyScheduleToSlackJob struct {
	syncOpts config.ScheduleSyncOptions // options for tasks during sync
	schedule cron.Schedule              // on which this job runs
	dryrun   bool                       // when enabled changes are not manifested
	err      error                      // err used for slack info message

	pd          *pagerdutyclient.Client // pagerduty API access
	slackClient *slackclient.Client     // slack API access

	slackHandle string // of the target user group
	// TODO: get pagerdutySchedules when creating the Job?
	pagerDutyIDs       []string              // IDs of the schedules to sync
	pagerdutyUsers     []pagerduty.User      // users part of the pagerduty schedule(s)
	pagerdutySchedules []pagerduty.APIObject // pagerduty schedules synced by this job
}

// NewSchedulesSyncJob creates a new job to sync members of pagerduty schedules to a slack user group
func NewScheduleSyncJob(cfg config.PagerdutyScheduleOnDutyToSlackGroup, dryrun bool, pd *pagerdutyclient.Client, slackClient *slackclient.Client) (*PagerdutyScheduleToSlackJob, error) {
	schedule, err := cron.ParseStandard("TZ=UTC " + cfg.CrontabExpressionForRepetition)
	if err != nil {
		return nil, fmt.Errorf("job: invalid cron schedule '%s': %w", cfg.CrontabExpressionForRepetition, err)
	}
	return &PagerdutyScheduleToSlackJob{
		syncOpts:     cfg.SyncOptions,
		dryrun:       dryrun,
		slackHandle:  cfg.ObjectsToSync.SlackGroupHandle,
		pagerDutyIDs: cfg.ObjectsToSync.PagerdutyObjectIDs,
		schedule:     schedule,
		pd:           pd,
		slackClient:  slackClient,
	}, nil
}

// Run syncs pagerduty schedule members to slack user group
func (s *PagerdutyScheduleToSlackJob) Run() error {
	log.Info(s.Name())

	tfF, err := time.ParseDuration(s.syncOpts.HandoverTimeFrameForward)
	if err != nil {
		tfF = time.Nanosecond * 0
		eS := fmt.Sprintf("job: invalid timeframe forward duration '%s' for sync of '%s'. Use default 0.:%s", s.syncOpts.HandoverTimeFrameBackward, s.slackHandle, err.Error())
		s.err = fmt.Errorf(eS)
	}
	tfB, err := time.ParseDuration(s.syncOpts.HandoverTimeFrameBackward)
	if err != nil {
		tfB = time.Nanosecond * 0
		eS := fmt.Sprintf("job: invalid timeframe backward duration '%s' for sync of '%s'. Use default 0.:%s", s.syncOpts.HandoverTimeFrameBackward, s.slackHandle, err.Error())
		log.Warn(eS)
		log.Info(eS)
	}

	pdUsers, pdSchedules, err := s.pd.ListOnCallUsers(s.pagerDutyIDs, tfF, tfB, s.syncOpts.SyncStyle)
	if err != nil {
		s.err = err
		return err
	}
	s.pagerdutyUsers = pdUsers
	s.pagerdutySchedules = pdSchedules

	// get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
	slackUsers, err := s.slackClient.MatchPDUsers(pdUsers)
	if err != nil {
		s.err = err
		return err
	}

	// put ldap users which also have a slack account to our slack group (who's not in the ldap group is out)
	if _, err = s.slackClient.AddToGroup(s.slackHandle, slackUsers, s.dryrun); err != nil {
		s.err = err
		return fmt.Errorf("job: adding OnDuty members to slack group %s failed: %w", s.slackHandle, err)
	}
	return nil
}

// Name of the job
func (s *PagerdutyScheduleToSlackJob) Name() string {
	return fmt.Sprintf("job: sync pagerduty schedule(s) '%s' to slack group: '%s'", strings.Join(s.pagerDutyIDs, ","), s.slackHandle)
}

// Icon returns name of icon to show in Slack messages
func (s *PagerdutyScheduleToSlackJob) Icon() string {
	return ":calendar:"
}

// JobType as string
func (s *PagerdutyScheduleToSlackJob) JobType() string {
	return string(PdScheduleSync)
}

// SlackHandle of the slack user group
func (s *PagerdutyScheduleToSlackJob) SlackHandle() string {
	return s.slackHandle
}

// PagerDutyObjects returns the pagerduty schedule/teams synced
func (s *PagerdutyScheduleToSlackJob) PagerDutyObjects() []pagerduty.APIObject {
	return s.pagerdutySchedules
}

// SlackInfoMessageBody returns TexBlock with the pagerduty users on shift
func (s *PagerdutyScheduleToSlackJob) SlackInfoMessageBody() *slack.TextBlockObject {
	var sL []string
	for _, aO := range s.pagerdutyUsers {
		sL = append(sL, fmt.Sprintf("<%s|%s>", aO.HTMLURL, aO.Summary))
	}

	return &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf("*Who is on shift:*\n - %s", strings.Join(sL, ",\n -")),
		Emoji:    false,
		Verbatim: false,
	}
}

// Dryrun is true when the job is not performing changes
func (s *PagerdutyScheduleToSlackJob) Dryrun() bool {
	return s.dryrun
}

// NextRun returns the time from now when the cron is next executed
func (s *PagerdutyScheduleToSlackJob) NextRun() time.Time {
	return s.schedule.Next(time.Now())
}

// Error if any occurred during the sync
func (s *PagerdutyScheduleToSlackJob) Error() error {
	return s.err
}
