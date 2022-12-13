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
	syncOpts           config.ScheduleSyncOptions
	dryrun             bool
	slackHandle        string
	pagerDutyIDs       []string
	pagerdutyUsers     []pagerduty.User
	pagerdutySchedules []pagerduty.APIObject
	schedule           cron.Schedule
	pd                 *pagerdutyclient.PDClient
	slackClient        *slackclient.SlackClient
	err                error
}

func NewScheduleSyncJob(cfg config.PagerdutyScheduleOnDutyToSlackGroup, dryrun bool, pd *pagerdutyclient.PDClient, slackClient *slackclient.SlackClient) (*PagerdutyScheduleToSlackJob, error) {
	schedule, err := cron.ParseStandard(cfg.CrontabExpressionForRepetition)
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

func (s *PagerdutyScheduleToSlackJob) Run() error {
	log.Info(s.Name())

	tfF, err := time.ParseDuration(s.syncOpts.HandoverTimeFrameForward)
	if err != nil {
		tfF = time.Nanosecond * 0
		eS := fmt.Sprintf("Invalid duration given in job %s: %s", s.slackHandle, s.syncOpts.HandoverTimeFrameForward)
		s.err = fmt.Errorf(eS)
		log.Info(eS)
	}
	tfB, err := time.ParseDuration(s.syncOpts.HandoverTimeFrameBackward)
	if err != nil {
		tfB = time.Nanosecond * 0
		eS := fmt.Sprintf("job: invalid duration '%s' for sync of '%s'. Use default 0.:%s", s.syncOpts.HandoverTimeFrameBackward, s.slackHandle, err.Error())
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
		return fmt.Errorf("adding OnDuty members to slack group failed: %w", err)
	}
	return nil
}

func (s *PagerdutyScheduleToSlackJob) Icon() string {
	return ":calendar:"
}

func (s *PagerdutyScheduleToSlackJob) Error() error {
	return s.err
}

func (s *PagerdutyScheduleToSlackJob) Dryrun() bool {
	return s.dryrun
}

func (s *PagerdutyScheduleToSlackJob) NextRun() time.Time {
	return s.schedule.Next(time.Now())
}

func (s *PagerdutyScheduleToSlackJob) PagerDutyObjects() []pagerduty.APIObject {
	return s.pagerdutySchedules
}

func (s *PagerdutyScheduleToSlackJob) SlackHandleID() string {
	return s.slackHandle
}

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

func (s *PagerdutyScheduleToSlackJob) Name() string {
	return fmt.Sprintf("job: sync pagerduty schedule(s) '%s' to slack group: '%s'", strings.Join(s.pagerDutyIDs, ","), s.slackHandle)
}

func (s *PagerdutyScheduleToSlackJob) JobType() string {
	return string(config.PdScheduleSync)
}
