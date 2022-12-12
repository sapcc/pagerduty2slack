package slack

import (
	"fmt"
	"strings"
	"sync"

	"github.com/PagerDuty/go-pagerduty"
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

	return LoadMasterData()
}

// LoadMasterData singleton master data to speed up
func LoadMasterData() (err error) {
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
	if slackGroupHandle == "" {
		return slack.UserGroup{}, fmt.Errorf("slack: finding group failed, empty handle")
	}

	// TODO: do we need this?
	for _, group := range slackGrps {
		log.Debugf("slack group: ID: %s, Name: %s, Count: %d (DateDeleted: %s) - %s\n", group.ID, group.Name, group.UserCount, group.DateDelete, group.Description)
	}

	// get the group we are interested in
	var targetGroup slack.UserGroup
	for _, g := range slackGrps {
		if strings.EqualFold(g.Handle, slackGroupHandle) {
			targetGroup = g
			break
		}
	}

	if targetGroup.Handle == "" {
		log.Errorf("slack: finding group handle '%s' failed. check config", slackGroupHandle)
		return slack.UserGroup{}, fmt.Errorf("slack: finding group handle '%s' failed. check config", slackGroupHandle)
	}

	return targetGroup, nil
}

// MatchPDUsers returns slack users matching the given pagerduty users
func MatchPDUsers(pdUsers []pagerduty.User) ([]slack.User, error) {
	// if no pdUsers given, we don't need to filter
	if pdUsers == nil {
		log.Warn("empty PD user list given!")
		return nil, fmt.Errorf("empty PD user list; check shift schedule")
	}

	// get all SLACK User Ids which are in our PD Group - some people are not in slack
	userList := matchPDToSlackUsers(pdUsers, slackUserList)

	log.Infof("slack: #%d user(s) in PD user group | #%d user(s) in all of slack | #%d user(s) will be in slack group\n", len(pdUsers), len(slackUserList), len(userList))
	return userList, nil
}

// AddToGroup sets an array of Slack User to an Slack Group (found by name), returns true if noop
func AddToGroup(jI *config.JobInfo, slackUsers []slack.User) (noChange bool, err error) {
	var bNoChange = true

	// get the group we are interested in
	userGroup, err := GetSlackGroup(jI.SlackHandleID())
	if err != nil {
		return true, fmt.Errorf("slack: retrieving slack group '%s' failed: %w", jI.SlackHandleID(), err)
	}
	jI.SlackGroupObject = userGroup

	if len(slackUsers) == 0 {
		log.Warnf("slack: user list is empty; nothing to update")
		jI.Error = fmt.Errorf("slack: user list empty; no update done")
		return true, nil
	}

	log.Infof("slack: target group %s[%s]", jI.SlackGroupObject.ID, jI.SlackGroupObject.Name)

	// we need a list of IDs
	var slackUserIds []string

	if len(slackUsers) == len(jI.SlackGroupObject.Users) {
		for _, user := range slackUsers {
			if !groupContainsUser(jI.SlackGroupObject.Users, user) {
				slackUserIds = append(slackUserIds, user.ID)
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
	log.Infof("slack: group %s has '%d' members(s)", jI.SlackGroupObject.Name, len(slackUsers))
	return bNoChange, nil
}

func DisableGroup(jI *config.JobInfo) {
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

// groupContainsUser returns true user is contained in the groupUserIDs
func groupContainsUser(groupUserIDs []string, user slack.User) bool {
	if len(groupUserIDs) == 0 {
		return false
	}
	for _, id := range groupUserIDs {
		if id == user.ID {
			return true
		}
	}
	return false
}

// matchPDToSlackUsers returns a list of valid Slack users that match the list of PagerDuty users
func matchPDToSlackUsers(pdUsers []pagerduty.User, slackUsers []slack.User) []slack.User {
	var matchedSlackUsers []slack.User
	for _, pd := range pdUsers {
		if pd.Email == "" {
			log.Infof("pagerduty: skipping user %s, no email assigned", pd.Name)
			continue
		}
		for _, slack := range slackUsers {
			if slack.Deleted {
				continue
			}
			if strings.EqualFold(pd.Email, slack.Profile.Email) {
				matchedSlackUsers = append(matchedSlackUsers, slack)
			}
		}
	}
	return matchedSlackUsers
}
