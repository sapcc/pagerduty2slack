package config

import (
    "fmt"
    "strings"
    "time"

    "github.com/PagerDuty/go-pagerduty"
    "github.com/ahmetb/go-linq"
    "github.com/robfig/cron"
    "github.com/slack-go/slack"
)

type ObjectSyncType string

const (
    PdScheduleSync ObjectSyncType = "PD Schedule"
    PdTeamSync                    = "PD Team"
)

type JobInfo struct {
    Error            error
    Cfg              Config
    JobCounter       int
    PdObjects        []pagerduty.APIObject
    PdObjectMember   []pagerduty.User
    SlackGroupObject slack.UserGroup
    SlackGroupUser   []slack.User
    WriteChanges     bool
    JobType          ObjectSyncType
    ObjectsToSync    SyncObjects
    CronJobId        cron.EntryID
    CronObject       *cron.Cron
}

func (jIS JobInfo) SlackHandleId() string {
    if jIS.JobType == PdTeamSync {
        return jIS.Cfg.Jobs.TeamSync[jIS.JobCounter].ObjectsToSync.SlackGroupHandle
    }
    return jIS.Cfg.Jobs.ScheduleSync[jIS.JobCounter].ObjectsToSync.SlackGroupHandle
}
func (jIS JobInfo) PagerDutyIds() []string {

    if jIS.JobType == PdTeamSync {
        return jIS.Cfg.Jobs.TeamSync[jIS.JobCounter].ObjectsToSync.PagerdutyObjectId
    }
    return jIS.Cfg.Jobs.ScheduleSync[jIS.JobCounter].ObjectsToSync.PagerdutyObjectId

}
func (jIS JobInfo) getSlackUserNames(asLink bool) []string {
    var uN []string
    if jIS.SlackGroupObject.ID == "" {
        return uN
    }
    if asLink {
        linq.From(jIS.SlackGroupObject.Users).SelectT(func(u string) string {
            return fmt.Sprintf("https://%s.slack.com/user/@%s", jIS.Cfg.Slack.Workspace, u)
        }).ToSlice(&uN)
        return uN
    }

    return jIS.SlackGroupObject.Users
}
func (jIS JobInfo) getIcon() string {
    if jIS.JobType == PdTeamSync {
        return ":threepeople:"
    }
    return ":calendar:"
}

func (jIS JobInfo) JobName() string {
    return fmt.Sprintf("%s for PD: '%s' > Slack: '%s'", jIS.JobType, strings.Join(jIS.PagerDutyIds()[:], ","), jIS.SlackHandleId())
}

func (jIS JobInfo) NextRun() time.Time {
    return jIS.CronObject.Entry(jIS.CronJobId).Next
}
func (jIS JobInfo) LastRun() time.Time {
    return jIS.CronObject.Entry(jIS.CronJobId).Prev
}

func (jIS JobInfo) GetSlackInfoMessage() slack.MsgOption {

    divSection := slack.NewDividerBlock()

    sHeaderText := fmt.Sprintf("%s %s > Slack `%s`", jIS.getIcon(), jIS.JobType, jIS.SlackHandleId())
    if !jIS.WriteChanges {
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
    //fields = append(fields, &slack.TextBlockObject{
    //	Type:     slack.MarkdownType,
    //	Text:     fmt.Sprintf("*Member Count:*\nPagerDuty: `%d` / Slack: `%d`", len(jIS.PdObjectMember), len(jIS.SlackGroupObject.Users)),
    //	Emoji:    false,
    //	Verbatim: false,
    //})
    var uL []string
    linq.From(jIS.SlackGroupUser).SelectT(func(u slack.User) string {
        return u.Profile.DisplayNameNormalized
    }).ToSlice(&uL)
    fields = append(fields, &slack.TextBlockObject{
        Type:     slack.MarkdownType,
        Text:     fmt.Sprintf("*Who is on shift:*\n -%s", strings.Join(uL, ",\n -")),
        Emoji:    false,
        Verbatim: false,
    })

    //if jIS.JobType == PdScheduleSync {
    //    fields = append(fields, &slack.TextBlockObject{
    //        Type:     slack.MarkdownType,
    //        Text:     fmt.Sprintf("*On Duty:*\n-%s", strings.Join(jIS.GetSlackUserNames(true), "\n- ")),
    //        Emoji:    false,
    //        Verbatim: false,
    //    })
    //}
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
