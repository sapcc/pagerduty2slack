package slackclient

import (
	"fmt"
	"strings"
	"sync"

	"github.com/PagerDuty/go-pagerduty"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"

	"github.com/sapcc/pagerduty2slack/internal/config"
)

type SlackClientType int

const (
	SlackClientTypeBot SlackClientType = iota
	SlackClientTypeUser
)

type SlackClient struct {
	botClient     *slack.Client
	userClient    *slack.Client
	users         []slack.User
	groups        []slack.UserGroup
	infoChannel   *slack.Channel
	infoChannelID string
}

// newAPIClient provides token specific new slack client object
func newAPIClient(cfg *config.SlackConfig, sct SlackClientType, options ...slack.Option) (*slack.Client, error) {
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
func (c *SlackClient) PostBlocksMessage(blocks ...slack.Block) error {
	msO := slack.MsgOptionBlocks(blocks...)
	return c.PostMessage(msO)
}

func (c *SlackClient) PostMessage(msO slack.MsgOption) error {
	if _, _, err := c.botClient.PostMessage(c.infoChannel.ID, msO); err != nil {
		return fmt.Errorf("slack: failed posting message: %w", err)
	}
	log.Debug("Message successfully sent to channel ", c.infoChannel.Name)
	return nil
}

// New inits a slackclient with bot and user client and loads masterdata
func New(cfg *config.SlackConfig) (*SlackClient, error) {
	bot, err := newAPIClient(cfg, SlackClientTypeBot)
	if err != nil {
		return nil, fmt.Errorf("slack: failed creating bot client: %w", err)
	}
	user, err := newAPIClient(cfg, SlackClientTypeUser)
	if err != nil {
		return nil, fmt.Errorf("slack: failed creating user client: %w", err)
	}

	c := &SlackClient{
		botClient:     bot,
		userClient:    user,
		infoChannelID: cfg.InfoChannelID,
	}

	err = c.LoadMasterData()
	if err != nil {
		return nil, fmt.Errorf("slack: failed loading masterdata: %w", err)
	}
	return c, nil
}

// LoadMasterData singleton master data to speed up
func (c *SlackClient) LoadMasterData() (err error) {
	slackChannelsTemp, err := c.botClient.GetConversationInfo(c.infoChannelID, true)
	if err != nil {
		return fmt.Errorf("slack: failed retrieving info channel '%s': %w", c.infoChannelID, err)
	}
	c.infoChannel = slackChannelsTemp

	slackUserListTemp, err := c.botClient.GetUsers()
	if err != nil {
		return fmt.Errorf("slack: failed retrieving users: %w", err)
	}

	slackGrpsTemp, err := c.botClient.GetUserGroups(slack.GetUserGroupsOptionIncludeUsers(true))
	if err != nil {
		return fmt.Errorf("slack: failed retrieving user groups: %w", err)
	}

	var mutex = &sync.Mutex{}
	mutex.Lock()
	c.users = slackUserListTemp
	c.groups = slackGrpsTemp
	mutex.Unlock()
	log.Debug("slack: masterdata successfully updated")
	return nil
}

// GetSlackGroup requests existing Group for given name
func (c *SlackClient) GetSlackGroup(slackGroupHandle string) (slack.UserGroup, error) {
	if slackGroupHandle == "" {
		return slack.UserGroup{}, fmt.Errorf("slack: finding group failed, empty handle")
	}

	// get the group we are interested in
	var targetGroup slack.UserGroup
	for _, g := range c.groups {
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
func (c *SlackClient) MatchPDUsers(pdUsers []pagerduty.User) ([]slack.User, error) {
	// if no pdUsers given, we don't need to filter
	if pdUsers == nil {
		log.Warn("empty PD user list given!")
		return nil, fmt.Errorf("empty PD user list; check shift schedule")
	}

	// get all SLACK User Ids which are in our PD Group - some people are not in slack
	userList := c.matchPDToSlackUsers(pdUsers)

	log.Infof("slack: found #%v matching slack user(s) for #%v user(s) in PD group", len(userList), len(pdUsers))
	return userList, nil
}

// AddToGroup sets an array of Slack User to an Slack Group (found by name), returns true if noop
func (c *SlackClient) AddToGroup(groupHandle string, slackUsers []slack.User, dryrun bool) (noChange bool, err error) {
	noChange = true

	// get the group we are interested in
	userGroupBefore, err := c.GetSlackGroup(groupHandle)
	if err != nil {
		return true, fmt.Errorf("slack: retrieving slack group '%s' failed: %w", groupHandle, err)
	}

	if len(slackUsers) == 0 {
		return true, fmt.Errorf("slack: user list empty; no update done")
	}

	// we need a list of IDs
	var slackUserIds []string
	if len(slackUsers) == len(userGroupBefore.Users) {
		for _, user := range slackUsers {
			if !groupContainsUser(userGroupBefore.Users, user) {
				slackUserIds = append(slackUserIds, user.ID)
				noChange = false
				continue
			}
		}
	} else {
		noChange = false
	}

	var userGroupAfter slack.UserGroup
	if !dryrun && !noChange {
		userGroupAfter, err = c.userClient.UpdateUserGroupMembers(userGroupBefore.ID, strings.Join(slackUserIds, ","))
		if err != nil {
			return noChange, fmt.Errorf("slack: writing changes for user group %s[%s] failed: %s", userGroupBefore.Name, userGroupBefore.ID, err.Error())
		}

		log.Infof("slack: updated %s successfully", userGroupAfter.Name)

		if userGroupAfter.DateDelete.String() == "" {
			_, err = c.userClient.EnableUserGroup(userGroupAfter.ID)
			if err != nil {
				return noChange, fmt.Errorf("slack: enabling user group %s[%s] failed: %s", userGroupBefore.Name, userGroupBefore.ID, err.Error())
			}
		}
	} else {
		userGroupAfter = userGroupBefore
	}

	var removedUsers []string
	if !noChange {
		for _, u := range userGroupBefore.Users {
			var removed = true
			for _, n := range userGroupAfter.Users {
				if u == n {
					removed = false
					break
				}
			}
			if removed {
				removedUsers = append(removedUsers, u)
			}
		}
	}

	if dryrun {
		log.Infof("slack: dry run. no changes executed.")
	}
	log.Infof("slack: added %v to and removed %v from group '%s'(%d member(s))", slackUserIds, removedUsers, userGroupAfter.Name, len(userGroupAfter.Users))

	return noChange, nil
}

func (c *SlackClient) DisableGroup(groupID string) error {
	userGroup, err := c.userClient.DisableUserGroup(groupID)
	if err != nil {
		log.Errorf("slack: disabling slack user group %s failed: %s", groupID, err.Error())
		return err
	}
	log.Infof("slack: disabled slack user group %s[%s]", userGroup.Name, userGroup.ID)
	return nil
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
func (c *SlackClient) matchPDToSlackUsers(pdUsers []pagerduty.User) []slack.User {
	var matchedSlackUsers []slack.User
	for _, pd := range pdUsers {
		if pd.Email == "" {
			log.Infof("pagerduty: skipping user %s, no email assigned", pd.Name)
			continue
		}
		for _, slack := range c.users {
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