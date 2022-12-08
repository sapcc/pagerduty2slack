package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/ahmetb/go-linq"
	"github.com/robfig/cron/v3"
	"github.com/slack-go/slack"
)

type ObjectSyncType string

const (
	PdScheduleSync ObjectSyncType = "PD Schedule"
	PdTeamSync     ObjectSyncType = "PD Team"
)

type JobInfo struct {
	Error                        error                 `json:"error,omitempty"`
	Cfg                          Config                `json:"cfg"`
	JobCounter                   int                   `json:"job_counter,omitempty"`
	JobRunComment                string                `json:"job_run_comment,omitempty"`
	PdObjects                    []pagerduty.APIObject `json:"pd_objects,omitempty"`
	PdObjectMember               []pagerduty.User      `json:"pd_object_member,omitempty"`
	PdObjectMemberWithoutContact []pagerduty.User      `json:"pd_object_member_without_contact,omitempty"`
	PdObjectMemberWithoutSlack   []pagerduty.User      `json:"pd_object_member_without_slack,omitempty"`
	SlackGroupObject             slack.UserGroup       `json:"slack_group_object"`
	SlackGroupUser               []slack.User          `json:"slack_group_user,omitempty"`
	JobType                      ObjectSyncType        `json:"job_type,omitempty"`
	CronJobID                    cron.EntryID          `json:"cron_job_id,omitempty"`
	CronObject                   *cron.Cron            `json:"cron_object,omitempty"`
}

func (jIS JobInfo) SlackHandleID() string {
	if jIS.JobType == PdTeamSync {
		return jIS.Cfg.Jobs.TeamSync[jIS.JobCounter].ObjectsToSync.SlackGroupHandle
	}
	return jIS.Cfg.Jobs.ScheduleSync[jIS.JobCounter].ObjectsToSync.SlackGroupHandle
}
func (jIS JobInfo) PagerDutyIDs() []string {
	if jIS.JobType == PdTeamSync {
		return jIS.Cfg.Jobs.TeamSync[jIS.JobCounter].ObjectsToSync.PagerdutyObjectID
	}
	return jIS.Cfg.Jobs.ScheduleSync[jIS.JobCounter].ObjectsToSync.PagerdutyObjectID
}

func (jIS JobInfo) getIcon() string {
	if jIS.JobType == PdTeamSync {
		return ":threepeople:"
	}
	return ":calendar:"
}

func (jIS JobInfo) JobName() string {
	return fmt.Sprintf("%s for PD: '%s' > Slack: '%s'", jIS.JobType, strings.Join(jIS.PagerDutyIDs()[:], ","), jIS.SlackHandleID())
}

func (jIS JobInfo) NextRun() time.Time {
	return jIS.CronObject.Entry(jIS.CronJobID).Next
}
func (jIS JobInfo) LastRun() time.Time {
	return jIS.CronObject.Entry(jIS.CronJobID).Prev
}

func (jIS JobInfo) GetSlackInfoMessage() slack.MsgOption {
	divSection := slack.NewDividerBlock()

	sHeaderText := fmt.Sprintf("%s %s > Slack Handle: `%s`", jIS.getIcon(), jIS.JobType, jIS.SlackHandleID())
	if !jIS.Cfg.Global.Write {
		sHeaderText += " - !!! DRY RUN !!! No update done !!!"
	}
	headerText := slack.NewTextBlockObject(slack.MarkdownType, sHeaderText, false, false)
	headerSection := slack.NewSectionBlock(headerText, nil, nil)

	var errorText *slack.TextBlockObject
	var errorSection *slack.SectionBlock
	if jIS.Error != nil {
		errorText = slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf(":stop-sign: *Error:* %s", jIS.Error), false, false)
		errorSection = slack.NewSectionBlock(errorText, nil, nil)
	}

	var fields []*slack.TextBlockObject
	var sL []string
	for _, aO := range jIS.PdObjects {
		sL = append(sL, fmt.Sprintf("<%s|%s>", aO.HTMLURL, aO.Summary))
	}
	fields = append(fields, &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf("*PD Source*\n%s", strings.Join(sL, "\n")),
		Emoji:    false,
		Verbatim: false,
	})

	if jIS.JobType == PdScheduleSync {
		fields = append(fields, jIS.getSlackInfoMessageBodyScheduleSync())
	} else {
		fields = append(fields, jIS.getSlackInfoMessageBodyTeamSync())
	}

	fields = append(fields, &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf(":alarm_clock: *Next run:* %s", jIS.NextRun().Format(time.RFC822)),
		Emoji:    false,
		Verbatim: false,
	})
	//jobSection := slack.NewSectionBlock(jobText, fields, nil)
	jobSection := slack.NewSectionBlock(nil, fields, nil)

	if errorSection != nil {
		return slack.MsgOptionBlocks(headerSection, errorSection, jobSection, divSection)
	}
	return slack.MsgOptionBlocks(headerSection, jobSection, divSection)
}

func (jIS JobInfo) getSlackInfoMessageBodyTeamSync() *slack.TextBlockObject {
	return &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf("*Member Count:*\n `%d` are in this Slack group", len(jIS.SlackGroupObject.Users)),
		Emoji:    false,
		Verbatim: false,
	}
}
func (jIS JobInfo) getSlackInfoMessageBodyScheduleSync() *slack.TextBlockObject {
	var uL []string
	linq.From(jIS.SlackGroupUser).SelectT(func(u slack.User) string {
		return u.Profile.DisplayNameNormalized
	}).ToSlice(&uL)

	var sL []string
	for _, aO := range jIS.PdObjectMember {
		sL = append(sL, fmt.Sprintf("<%s|%s>", aO.HTMLURL, aO.Summary))
	}

	return &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf("*Who is on shift:*\n - %s", strings.Join(sL, ",\n -")),
		Emoji:    false,
		Verbatim: false,
	}
}
