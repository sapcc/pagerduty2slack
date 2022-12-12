package slackclient

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

// Bot is the struct for the slack bot.
type Bot struct {
	client    *slack.Client
	rtmClient *slack.RTM
	// channelID   string
	// helpCommand Command
	commands []Command
}

func NewEventBot(c *SlackClient) (*Bot, error) {
	b := &Bot{
		client:    c.botClient,
		rtmClient: c.botClient.NewRTM(),
		//botID:      cfg.BotID,
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
}

func (b *Bot) StartListening() {
	// Listen to slack events.
	go b.rtmClient.ManageConnection()

	for {
		msg := <-b.rtmClient.IncomingEvents
		log.Debug("msg", "received slack event", "type", msg.Type)

		switch e := msg.Data.(type) {
		case *slack.MessageEvent:
			b.handleMessageEvent(e)

		case *slack.RTMError:
			log.Error("msg", "slack RTM error", "err", e.Error())

		case *slack.InvalidAuthEvent:
			log.Error("msg", "slack authentication failed")

		case *slack.ConnectionErrorEvent:
			log.Error("error connecting to slack", "err", e.Error())
		default:
			log.Warn("unexpected message data for type", msg.Type)
		}
	}
}

func (b *Bot) handleMessageEvent(e *slack.MessageEvent) {
	info := b.rtmClient.GetInfo()
	prefix := fmt.Sprintf("<@%s>", info.User.ID)

	if !strings.HasPrefix(e.Text, prefix) {
		return
	}

	// Only respond if the bot is mentioned.
	text := e.Msg.Text
	text = strings.TrimPrefix(text, prefix)
	text = strings.TrimSpace(text)
	text = strings.ToLower(text)

	// Update original message text with normalized one.
	e.Msg.Text = text

	atLeastOneCommand := false
	for _, c := range b.commands {
		//if util.HasAnyPrefix(c.Keywords(), text) {

		log.Debug("msg", "running command", "description", c.Describe())
		atLeastOneCommand = true
		/*response, err := c.Run(&e.Msg)
		  if err != nil {
		      return err
		  }
		*/
		/*if err := b.respond(response, &e.Msg); err != nil {
		    return err
		}*/
		//}
	}

	// Show the help if no command could be found.
	if atLeastOneCommand {
		return
	}

	/*response, err := b.helpCommand.Run(&e.Msg)
	  if err != nil {
	      return err
	  }*/

	// return //b.respond(response, &e.Msg)
}
