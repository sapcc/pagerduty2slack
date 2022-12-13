package jobs

import (
	"fmt"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/slack-go/slack"

	slackclient "github.com/sapcc/pagerduty2slack/internal/clients/slack"
)

type SyncJob interface {
	Name() string
	Icon() string
	JobType() string
	SlackHandleID() string
	PagerDutyObjects() []pagerduty.APIObject
	SlackInfoMessageBody() *slack.TextBlockObject
	Dryrun() bool
	NextRun() time.Time
	Error() error
}

func PostInfoMessage(c *slackclient.SlackClient, j SyncJob) error {
	divSection := slack.NewDividerBlock()

	sHeaderText := fmt.Sprintf("%s %s > Slack Handle: `%s`", j.Icon(), j.JobType(), j.SlackHandleID())
	if j.Dryrun() {
		sHeaderText += " - !!! DRY RUN !!! No update done !!!"
	}
	headerText := slack.NewTextBlockObject(slack.MarkdownType, sHeaderText, false, false)
	headerSection := slack.NewSectionBlock(headerText, nil, nil)

	var errorText *slack.TextBlockObject
	var errorSection *slack.SectionBlock
	if j.Error() != nil {
		errorText = slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf(":stop-sign: *Error:* %s", j.Error()), false, false)
		errorSection = slack.NewSectionBlock(errorText, nil, nil)
	}

	var fields []*slack.TextBlockObject
	var sL []string
	for _, aO := range j.PagerDutyObjects() {
		sL = append(sL, fmt.Sprintf("<%s|%s>", aO.HTMLURL, aO.Summary))
	}
	fields = append(fields, &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf("*PD Source*\n%s", strings.Join(sL, "\n")),
		Emoji:    false,
		Verbatim: false,
	})
	fields = append(fields, j.SlackInfoMessageBody())

	fields = append(fields, &slack.TextBlockObject{
		Type:     slack.MarkdownType,
		Text:     fmt.Sprintf(":alarm_clock: *Next run:* %s", j.NextRun().Format(time.RFC822)),
		Emoji:    false,
		Verbatim: false,
	})
	//jobSection := slack.NewSectionBlock(jobText, fields, nil)
	jobSection := slack.NewSectionBlock(nil, fields, nil)

	if errorSection != nil {
		return c.PostMessage(slack.MsgOptionBlocks(headerSection, errorSection, jobSection, divSection))
	}
	return c.PostMessage(slack.MsgOptionBlocks(headerSection, jobSection, divSection))
}
