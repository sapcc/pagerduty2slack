package clients

import (
	"fmt"
	"github.com/sapcc/pagerduty2slack/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"strings"
)

// Bot is the struct for the slack bot.
type Bot struct {
	client      *slack.Client
	app         *socketmode.Client
	jobs        *[]config.JobInfo
	channelID   string
	helpCommand Command
	commands    []Command
}

func NewEventBot(jobs *[]config.JobInfo) (*Bot, error) {

	b := &Bot{
		client: defaultSlackClientSocket,
		app:    socketmode.New(defaultSlackClientSocket, socketmode.OptionDebug(false)),
		jobs:   jobs,
	}

}

	for _, c := range availableCommands {
		cmd := c()
		if err := cmd.Init(); err != nil {
			log.Info("msg", "failed to initialize command", "keywords", strings.Join(cmd.Keywords(), ", "), "description", cmd.Describe(), "err", err.Error())
			continue
		}
		log.Info("msg", "registering command", "keywords", strings.Join(cmd.Keywords(), ", "), "description", cmd.Describe())
		b.commands = append(b.commands, cmd)
	}
	return b, nil
func (b *Bot) StartListening() {

	// Listen to slack events.
	go func() {
		for evt := range b.app.Events {

			switch evt.Type {
			case socketmode.EventTypeConnecting:
				fmt.Println("Connecting to Slack with Socket Mode...")
				_, _, err := b.client.PostMessage("C018LCC9230", slack.MsgOptionText("Yes, hello.", false))
				if err != nil {
					fmt.Printf("failed posting message: %v", err)
				}
			case socketmode.EventTypeConnectionError:
				fmt.Println("Connection failed. Retrying later...")
			case socketmode.EventTypeConnected:
				fmt.Println("Connected to Slack with Socket Mode.")
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					//var dat map[string]interface{}
					//fmt.Printf("Ignored %+v\n", json.Unmarshal(evt.Data,&dat))
					continue
				}

				fmt.Printf("Event received: %+v\n", eventsAPIEvent)

				b.app.Ack(*evt.Request)

				switch eventsAPIEvent.Type {
				case slackevents.CallbackEvent:
					innerEvent := eventsAPIEvent.InnerEvent
					switch ev := innerEvent.Data.(type) {
					case *slackevents.AppHomeOpenedEvent:

						var homeView = slack.HomeTabViewRequest{
							Type: slack.VTHomeTab,
						}
						// create the view using block-kit
						homeView.Blocks.BlockSet = append(homeView.Blocks.BlockSet,
							slack.NewSectionBlock(
								&slack.TextBlockObject{
									Type: slack.MarkdownType,
									Text: "Moin Hase",
								},
								nil,
								nil,
							),
						)

						_, err := b.client.PublishView(ev.User, homeView, "")
						if err != nil {
							log.Printf("ERROR publishHomeTabView: %v", err)
						}
					case *slackevents.AppMentionEvent:
						_, _, err := b.client.PostMessage(ev.Channel, slack.MsgOptionText("Yes, hello.", false))
						if err != nil {
							fmt.Printf("failed posting message: %v", err)
						}
					case *slackevents.MemberJoinedChannelEvent:
						fmt.Printf("user %q joined to channel %q", ev.User, ev.Channel)
					}
				default:
					b.client.Debugf("unsupported Events API event received")
				}
			case socketmode.EventTypeInteractive:
				callback, ok := evt.Data.(slack.InteractionCallback)
				if !ok {
					fmt.Printf("Ignored %+v\n", evt)

					continue
				}

				fmt.Printf("Interaction received: %+v\n", callback)

				var payload interface{}

				switch callback.Type {
				case slack.InteractionTypeBlockActions:
					// See https://api.slack.com/apis/connections/socket-implement#button
					b.client.PostMessage(callback.Channel.ID, slack.MsgOptionText("Click it baby.", false))
					b.client.Debugf("button clicked!")
				case slack.InteractionTypeShortcut:
				case slack.InteractionTypeViewSubmission:
					// See https://api.slack.com/apis/connections/socket-implement#modal
				case slack.InteractionTypeDialogSubmission:
				default:
				}

				b.app.Ack(*evt.Request, payload)

			case socketmode.EventTypeSlashCommand:
				cmd, ok := evt.Data.(slack.SlashCommand)
				log.Info(cmd)
				if !ok {
					fmt.Printf("Ignored %+v\n", evt)

					continue
				}

				var payload = slack.NewBlockMessage()

				switch cmd.Command {
				case "/whoisonduty":
					log.Debug(cmd.Command)
					for _, job := range *b.jobs {
						if job.JobType == "PdTeamSync" {
							payload.Blocks.BlockSet = append(payload.Blocks.BlockSet,
								slack.NewSectionBlock(
									&slack.TextBlockObject{
										Type: slack.MarkdownType,
										Text: fmt.Sprintf("%s %s %s", job.JobName(), job.Cfg.Jobs.ScheduleSync[job.JobCounter].ObjectsToSync.SlackGroupHandle, job.CronObject.Entry(job.CronJobId).Next),
									},
									nil,
									nil,
								),
							)
						}

					}
					b.app.Ack(*evt.Request, payload)
				case "/listsyncjobs":
					log.Debug(cmd.Command)
					for rc, job := range *b.jobs {
						if rc == 0 {
							continue
						}
						payload.Blocks.BlockSet = append(payload.Blocks.BlockSet,
							slack.NewSectionBlock(
								&slack.TextBlockObject{
									Type: slack.MarkdownType,
									Text: fmt.Sprintf("%s %s", job.JobName(), job.CronObject.Entry(job.CronJobId).Next),
								},
								nil,
								slack.NewAccessory(
									slack.NewButtonBlockElement(
										fmt.Sprintf("%d", job.CronJobId),
										fmt.Sprintf("%s", job.Cfg.Jobs.TeamSync[0].ObjectsToSync.SlackGroupHandle),
										&slack.TextBlockObject{
											Type: slack.PlainTextType,
											Text: "sync",
										},
									),
								),
							))
					}
					b.app.Ack(*evt.Request, payload)
				default:
					log.Info("need send the usage message")
				}

			default:
				log.Debug("Unexpected event type received: %s\n", evt.Type)
			}
		}
	}()
	if err := b.app.Run(); err != nil {
		log.Error(err)
	}
}
