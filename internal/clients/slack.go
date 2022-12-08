package clients

import (
	"fmt"
	"strings"
	"sync"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/ahmetb/go-linq"
	"github.com/pkg/errors"
	"github.com/sapcc/pagerduty2slack/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

var defaultSlackClientBot *slack.Client
var defaultSlackClientUser *slack.Client
var slackChannels []slack.Channel
var slackUserList []slack.User
var slackGrps []slack.UserGroup
var slackInfoChannel slack.Channel
var _slackInfoChannelName string

/* #region private methods */

type SlackClientType int

const (
	SlackClientTypeBot SlackClientType = iota
	SlackClientTypeUser
)

// NewSlackClient provides token specific new slack client object
func NewSlackClient(cfg config.SlackConfig, sct SlackClientType, options ...slack.Option) *slack.Client {
	var c *slack.Client
	options = append(options, slack.OptionDebug(false))

	if sct == SlackClientTypeUser {
		c = slack.New(cfg.UserSecurityToken, options...)
	} else {
		c = slack.New(cfg.BotSecurityToken, options...)
	}
	if c == nil {
		log.Panic("SLACK > Could not create Slack User Client - check Token / Connection / Weather (type: ", sct, ")")
	}
	return c
}

// PostBlocksMessage takes the blocks and sends them to the default info channel
func PostBlocksMessage(blocks ...slack.Block) error {
	msO := slack.MsgOptionBlocks(blocks...)
	return PostMessage(msO)
}
func PostMessage(msO slack.MsgOption) error {

	if _, _, err := defaultSlackClientBot.PostMessage(slackInfoChannel.ID, msO); err != nil {
		return err
	}

	log.Debug("Message successfully sent to channel ", slackInfoChannel.Name)
	return nil
}

/* #endregion */

// Init creates a slack client with given token
func Init(cfg config.SlackConfig) error {
	defaultSlackClientBot = NewSlackClient(cfg, SlackClientTypeBot)
	defaultSlackClientUser = NewSlackClient(cfg, SlackClientTypeUser)

	_slackInfoChannelName = cfg.InfoChannel

	return LoadSlackMasterData()
}

//LoadSlackMasterData singleton master data to speed up
func LoadSlackMasterData() (err error) {

	log.Debug("loadSlackMasterData running ...")

	slackChannelsTemp, err := GetChannels()

	slackUserListTemp, err := defaultSlackClientBot.GetUsers()
	if err != nil {
		return errors.Wrap(err, "get users failed")
	}

	slackGrpsTemp, err := defaultSlackClientBot.GetUserGroups(slack.GetUserGroupsOptionIncludeUsers(true))
	if err != nil {
		return errors.Wrap(err, "get user groups failed")
	}

	var mutex = &sync.Mutex{}
	mutex.Lock()
	slackChannels = slackChannelsTemp
	slackUserList = slackUserListTemp
	slackGrps = slackGrpsTemp
	mutex.Unlock()

	for _, c := range slackChannels {
		if c.GroupConversation.Name == _slackInfoChannelName {
			slackInfoChannel = c
			log.Debug("loadSlackMasterData updated.")
			return
		}
	}
	return fmt.Errorf("masterdata is missing channel %s", _slackInfoChannelName)
}

// GetSlackGroup requests existing Group for given name
func GetSlackGroup(slackGroupHandle string) (slack.UserGroup, error) {
	var targetGroup slack.UserGroup

	for _, group := range slackGrps {
		log.Debug(fmt.Sprintf("SLACK Group: ID: %s, Name: %s, Count: %d (DateDeleted: %s) - %s\n", group.ID, group.Name, group.UserCount, group.DateDelete, group.Description))
	}

	// get the group we are interested in
	q := linq.From(slackGrps).WhereT(func(group slack.UserGroup) bool {
		return strings.Compare(group.Handle, slackGroupHandle) == 0
	}).First()

	if q != nil {
		targetGroup = q.(slack.UserGroup)
	} else {
		log.Error("SLACK", ">", slackGroupHandle, " wasn't there @SLACK - check config!")
		return targetGroup, fmt.Errorf("slack handle %s doesn't exist; check config", slackGroupHandle)
	}

	return targetGroup, nil
}

// GetSlackUser delivers Slack User
func GetSlackUser(pdUsers []pagerduty.User) ([]slack.User, error) {

	// if no pdUsers given, we don't need to filter
	if pdUsers == nil {
		log.Warn("Empty PD user list given!")
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
		if log.GetLevel() == log.DebugLevel {
			log.Debug(fmt.Sprintf("SlackUser: %s - %s (%s)(deleted: %t)\n", u.ID, u.Name, u.Profile.DisplayName, u.Deleted))
		}
		return u
	}).ToSlice(&ul)

	log.Info(fmt.Printf("%d user in PD user group | %d in SLACK at all | %d user will be in SLACK group\n", len(pdUsers), len(slackUserList), len(ul)))

	return ul, nil
}

// SetSlackGroupUser sets an array of Slack User to an Slack Group (found by name)
//func SetSlackGroupUser(slackGroupHandle string, slackUser []slack.User, bWrite bool) error {
func SetSlackGroupUser(jI *config.JobInfo, slackUser []slack.User) bool {

	var bNoChange = true

	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("SLACK>%s (Searched Group: %s)", r.(error), jI.SlackHandleId()))
			jI.Error = r.(error)
		}
	}()

	// get the group we are interested in
	jI.SlackGroupObject, _ = GetSlackGroup(jI.SlackHandleId())

	if len(slackUser) == 0 {
		err := fmt.Errorf("user list was empty; no update done")
		log.Errorf("SLACK > %s :: %s", jI.SlackHandleId(), err)
		jI.Error = err
	}

	fmt.Println(fmt.Sprintf("SLACK>TargetGroup.ID: %s [%s]", jI.SlackGroupObject.ID, jI.SlackGroupObject.Name))

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

		//log.Debug(fmt.Sprintf("%s: %s",targetGroup.Name,strings.Join(slackUserIds, ","))

		userGroup, err := defaultSlackClientUser.UpdateUserGroupMembers(jI.SlackGroupObject.ID, strings.Join(slackUserIds, ","))
		if err != nil {
			log.Error("SLACK", "> SLACK error: ", err)
			jI.Error = err
		} else {
			log.Println("SLACK> changes were written!", userGroup)
		}
		if userGroup.DateDelete.String() == "" {
			_, err = defaultSlackClientUser.EnableUserGroup(jI.SlackGroupObject.ID)
			if err != nil {
				log.Error("SLACK", "> SLACK error: ", err)
				jI.Error = err
			}
		}

	} else {
		log.Println("SLACK> no changes were written, because flag 'bWrite' was set to 'false'")
	}

	log.Info(fmt.Sprintf("Group %s has `%d` member(s)", jI.SlackGroupObject.Name, len(slackUser)))

	return bNoChange
}

func DisableSlackGroup(jI *config.JobInfo) {
	userGroup, err := defaultSlackClientUser.DisableUserGroup(jI.SlackGroupObject.ID)
	if err != nil {
		log.Error("SLACK", "> SLACK error: ", err)
		jI.Error = err
	} else {
		log.Infof("SLACK> group `%s` disabled!", userGroup.Name)
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
		return nil, errors.Wrap(err, "get channels failed")
	}
	return channels, nil
}
