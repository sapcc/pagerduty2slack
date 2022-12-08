package clients

import (
	"fmt"
	"strings"
	"sync"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/ahmetb/go-linq"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"

	"github.com/sapcc/pagerduty2slack/internal/config"
)

var defaultSlackClientBot *slack.Client
var defaultSlackClientUser *slack.Client
var slackChannels []slack.Channel
var slackUserList []slack.User
var slackGrps []slack.UserGroup
var slackInfoChannel slack.Channel
var slackInfoChannelName string

type SlackClientType int

const (
	SlackClientTypeBot SlackClientType = iota
	SlackClientTypeUser
)

// NewSlackClient provides token specific new slack client object
func NewSlackClient(cfg config.SlackConfig, sct SlackClientType, options ...slack.Option) (*slack.Client, error) {
	var c *slack.Client
	options = append(options, slack.OptionDebug(false))

	switch sct {
	case SlackClientTypeUser:
		c = slack.New(cfg.UserSecurityToken, options...)
	case SlackClientTypeBot:
		c = slack.New(cfg.BotSecurityToken, options...)
	default:
		return nil, fmt.Errorf("slack: creating user client failed: ")
	}
	return c, nil
}

// PostBlocksMessage takes the blocks and sends them to the default info channel
func PostBlocksMessage(blocks ...slack.Block) error {
	msO := slack.MsgOptionBlocks(blocks...)
	return PostMessage(msO)
}

func PostMessage(msO slack.MsgOption) error {
	if _, _, err := defaultSlackClientBot.PostMessage(slackInfoChannel.ID, msO); err != nil {
		return fmt.Errorf("slack: failed posting message: %w", err)
	}
	log.Debug("Message successfully sent to channel ", slackInfoChannel.Name)
	return nil
}

// Init creates a slack client with given token
func Init(cfg config.SlackConfig) (err error) {
	defaultSlackClientBot, err = NewSlackClient(cfg, SlackClientTypeBot)
	if err != nil {
		return fmt.Errorf("slack: failed creating bot client: %w", err)
	}
	defaultSlackClientUser, err = NewSlackClient(cfg, SlackClientTypeUser)
	if err != nil {
		return fmt.Errorf("slack: failed creating user client: %w", err)
	}

	slackInfoChannelName = cfg.InfoChannel

	return LoadSlackMasterData()
}

// LoadSlackMasterData singleton master data to speed up
func LoadSlackMasterData() (err error) {
	log.Debug("slack: loading masterdata...")

	slackChannelsTemp, err := GetChannels()
	if err != nil {
		return fmt.Errorf("slack: failed retrieving channels: %w", err)
	}

	slackUserListTemp, err := defaultSlackClientBot.GetUsers()
	if err != nil {
		return fmt.Errorf("slack: failed retrieving users: %w", err)
	}

	slackGrpsTemp, err := defaultSlackClientBot.GetUserGroups(slack.GetUserGroupsOptionIncludeUsers(true))
	if err != nil {
		return fmt.Errorf("slack: failed retrieving user groups: %w", err)
	}

	var mutex = &sync.Mutex{}
	mutex.Lock()
	slackChannels = slackChannelsTemp
	slackUserList = slackUserListTemp
	slackGrps = slackGrpsTemp
	mutex.Unlock()

	for _, c := range slackChannels {
		if c.GroupConversation.Name == slackInfoChannelName {
			slackInfoChannel = c
			log.Debug("slack: masterdata successfully updated")
			return
		}
	}
	return fmt.Errorf("masterdata is missing channel %s", slackInfoChannelName)
}

// GetSlackGroup requests existing Group for given name
func GetSlackGroup(slackGroupHandle string) (slack.UserGroup, error) {
	var targetGroup slack.UserGroup

	for _, group := range slackGrps {
		log.Debugf("slack group: ID: %s, Name: %s, Count: %d (DateDeleted: %s) - %s\n", group.ID, group.Name, group.UserCount, group.DateDelete, group.Description)
	}

	// get the group we are interested in
	q := linq.From(slackGrps).WhereT(func(group slack.UserGroup) bool {
		return strings.Compare(group.Handle, slackGroupHandle) == 0
	}).First()

	if q != nil {
		var ok bool
		targetGroup, ok = q.(slack.UserGroup)
		if !ok {
			return slack.UserGroup{}, fmt.Errorf("type assertion for slack.UserGroup failed")
		}
	} else {
		log.Errorf("slack: finding group handle '%s' failed. check config", slackGroupHandle)
		return targetGroup, fmt.Errorf("slack: finding group handle '%s' failed. check config", slackGroupHandle)
	}

	return targetGroup, nil
}

// GetSlackUser delivers Slack User
func GetSlackUser(pdUsers []pagerduty.User) ([]slack.User, error) {
	// if no pdUsers given, we don't need to filter
	if pdUsers == nil {
		log.Warn("empty PD user list given!")
		return nil, fmt.Errorf("empty PD user list; check shift schedule")
	}

	// get all SLACK User Ids which are in our PD Group - some people are not in slack
	var ul []slack.User
	linq.From(slackUserList).WhereT(func(u slack.User) bool {
		return linq.From(pdUsers).WhereT(func(pU pagerduty.User) bool {
			//TODO: Proper handling of users who have no email set in PagerDuty. Also the case for inactive users
			return strings.Compare(strings.ToLower(pU.Email), strings.ToLower(u.Profile.Email)) == 0 && !u.Deleted && pU.Email != ""
		}).Count() > 0
	}).SelectT(func(u slack.User) slack.User {
		log.Debugf("slack: user %s - %s (%s)(deleted: %t)\n", u.ID, u.Name, u.Profile.DisplayName, u.Deleted)
		return u
	}).ToSlice(&ul)

	log.Info(fmt.Printf("%d user in PD user group | %d in SLACK at all | %d user will be in SLACK group\n", len(pdUsers), len(slackUserList), len(ul)))
	return ul, nil
}

// SetSlackGroupUser sets an array of Slack User to an Slack Group (found by name), returns true if noop
func SetSlackGroupUser(jI *config.JobInfo, slackUser []slack.User) (noChange bool, err error) {
	var bNoChange = true

	// get the group we are interested in
	userGroup, err := GetSlackGroup(jI.SlackHandleID())
	if err != nil {
		return true, fmt.Errorf("slack: retrieving slack group '%s' failed: %w", jI.SlackHandleID(), err)
	}
	jI.SlackGroupObject = userGroup

	if len(slackUser) == 0 {
		log.Warnf("slack: user list is empty; nothing to update")
		jI.Error = fmt.Errorf("slack: user list empty; no update done")
	}

	log.Infof("slack: target group %s[%s]", jI.SlackGroupObject.ID, jI.SlackGroupObject.Name)

	// we need a list of IDs
	var slackUserIds []string
	linq.From(slackUser).SelectT(func(u slack.User) string {
		return u.ID
	}).Distinct().ToSlice(&slackUserIds)

	if len(slackUser) == len(jI.SlackGroupObject.Users) {
		for _, user := range slackUser {
			if !linq.From(jI.SlackGroupObject.Users).Contains(user.ID) {
				bNoChange = false
				continue
			}
			bNoChange = true
		}
	} else {
		bNoChange = false
	}

	if jI.Cfg.Global.Write && !bNoChange {
		userGroup, err := defaultSlackClientUser.UpdateUserGroupMembers(jI.SlackGroupObject.ID, strings.Join(slackUserIds, ","))
		if err != nil {
			log.Errorf("slack: writing changes for user group %s[%s] failed: %s", jI.SlackGroupObject.Name, jI.SlackGroupObject.ID, err.Error())
			jI.Error = err
		} else {
			log.Infof("slack: updated %s successfully", userGroup.Name)
		}
		if userGroup.DateDelete.String() == "" {
			_, err = defaultSlackClientUser.EnableUserGroup(jI.SlackGroupObject.ID)
			if err != nil {
				log.Errorf("slack: enabling user group %s[%s] failed: %s", jI.SlackGroupObject.Name, jI.SlackGroupObject.ID, err.Error())
				jI.Error = err
			}
		}
	} else {
		log.Infof("slack: no changes executed. flag 'Write' is set to '%v'", jI.Cfg.Global.Write)
	}
	log.Infof("slack: group %s has '%d' members(s)", jI.SlackGroupObject.Name, len(slackUser))
	return bNoChange, nil
}

func DisableSlackGroup(jI *config.JobInfo) {
	userGroup, err := defaultSlackClientUser.DisableUserGroup(jI.SlackGroupObject.ID)
	if err != nil {
		log.Errorf("slack: disabling slack user group %s[%s] failed: %s", jI.SlackGroupObject.Name, jI.SlackGroupObject.ID, err)
		jI.Error = err
	} else {
		log.Infof("slack: disabled slack user group %s[%s]", userGroup.Name, userGroup.ID)
	}
}

// GetChannels gives the Channels with Members & co
func GetChannels() ([]slack.Channel, error) {
	cp := slack.GetConversationsParameters{
		ExcludeArchived: true,
		Types:           []string{"public_channel", "private_channel"},
		Limit:           1000,
	}

	channels, _, err := defaultSlackClientBot.GetConversations(&cp)
	if err != nil {
		return nil, fmt.Errorf("slack: retrieving channels failed: %w", err)
	}
	return channels, nil
}
