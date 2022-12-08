package clients

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/sapcc/pagerduty2slack/internal/config"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slacktest"
	"github.com/stretchr/testify/assert"
)

type slackTestData struct {
	userGroups []slack.UserGroup
	channels   []slack.Channel
	users      []slack.User
}

type responseMetadata struct {
	NextCursor string `json:"next_cursor"`
}

func setup() *slacktest.Server {
	testServer := slacktest.NewTestServer()
	go testServer.Start()

	users := []slack.User{
		createUserObject("spengler", "W012A3CDE", "T012AB3C4"),
		createUserObject("glinda", "W07QCRPA4", "T0G9PQBBK"),
	}

	testData := slackTestData{
		users: users,
		userGroups: []slack.UserGroup{
			createUserGroupObject("S0614TZR7", "T060RNRCH", "Team Admins", "admins", users),
			createUserGroupObject("S06158AV7", "T060RNRCH", "Team Owners", "owners", nil),
			createUserGroupObject("S0615G0KT", "T060RNRCH", "Marketing Team", "marketing", nil),
		},
		channels: []slack.Channel{
			createChannelObject("test", "123"),
			createChannelObject("general", "1337"),
		},
	}

	testServer.Handle("/conversations.list", testData.createListConversationsHandler)
	testServer.Handle("/users.list", testData.createListUsersHandler)
	testServer.Handle("/usergroups.list", testData.createListUserGroupsHandler)
	testServer.Handle("/usergroups.disable", testData.createDisableUserGroupsHandler)
	testServer.Handle("/usergroups.users.update", testData.createUpdateUserGroupsUserHandler)

	_slackInfoChannelName = "general"

	cfg := config.SlackConfig{
		UserSecurityToken: "TEST_TOKEN",
		BotSecurityToken:  "TEST_TOKEN",
	}
	apiURLOption := slack.OptionAPIURL(testServer.GetAPIURL())
	defaultSlackClientBot = NewSlackClient(cfg, SlackClientTypeBot, apiURLOption)
	defaultSlackClientUser = NewSlackClient(cfg, SlackClientTypeUser, apiURLOption)
	return testServer
}

func TestGetChannels(t *testing.T) {
	testServer := setup()
	defer testServer.Stop()
	channels, err := GetChannels()
	if assert.NoError(t, err) {
		assert.NotEmpty(t, channels)
	}
}

func TestGetSlackGroup(t *testing.T) {
	testServer := setup()
	defer testServer.Stop()
	LoadSlackMasterData()

	group, err := GetSlackGroup("admins")
	if assert.NoError(t, err) {
		assert.Equal(t, "Team Admins", group.Name)
	}
}

func TestLoadSlackMasterData(t *testing.T) {
	testServer := setup()
	defer testServer.Stop()

	assert.NoError(t, LoadSlackMasterData())
	assert.NotEmpty(t, slackChannels)
	assert.NotEmpty(t, slackUserList)
	assert.NotEmpty(t, slackGrps)
}

func TestGetSlackUser(t *testing.T) {
	pdUsers := []pagerduty.User{pagerduty.User{Email: "spengler@ghostbusters.example.com"}}
	slackUserList = []slack.User{
		slack.User{Profile: slack.UserProfile{Email: "spengler@ghostbusters.example.com"}},
		slack.User{Profile: slack.UserProfile{Email: "max@mustermann.example.com"}},
	}

	actualUsers, err := GetSlackUser(pdUsers)

	if assert.NoError(t, err) {
		assert.Len(t, actualUsers, 1)
		assert.Equal(t, "spengler@ghostbusters.example.com", actualUsers[0].Profile.Email)
	}
}

func TestSetSlackUserGroup(t *testing.T) {
	testServer := setup()
	defer testServer.Stop()

	LoadSlackMasterData()

	jobInfo := &config.JobInfo{JobType: config.PdTeamSync,
		Cfg: config.Config{
			Jobs: config.JobsConfig{
				TeamSync: []config.PagerdutyTeamToSlackGroup{
					config.PagerdutyTeamToSlackGroup{
						ObjectsToSync: config.SyncObjects{
							SlackGroupHandle: "admins"},
					},
				},
			},
			Global: config.GlobalConfig{
				Write: true,
			},
		},
	}
	slackUsers := []slack.User{slack.User{ID: "W012A3CDE"}}
	noChange := SetSlackGroupUser(jobInfo, slackUsers)

	assert.False(t, noChange)

}

func TestSetSlackUserGroupNoChange(t *testing.T) {
	testServer := setup()
	defer testServer.Stop()

	LoadSlackMasterData()

	jobInfo := &config.JobInfo{JobType: config.PdTeamSync,
		Cfg: config.Config{
			Jobs: config.JobsConfig{
				TeamSync: []config.PagerdutyTeamToSlackGroup{
					config.PagerdutyTeamToSlackGroup{
						ObjectsToSync: config.SyncObjects{
							SlackGroupHandle: "admins"},
					},
				},
			},
			Global: config.GlobalConfig{
				Write: false,
			},
		},
	}
	slackUsers := []slack.User{slack.User{ID: "W012A3CDE"}, slack.User{ID: "W07QCRPA4"}}
	noChange := SetSlackGroupUser(jobInfo, slackUsers)

	assert.True(t, noChange)
}

func TestDisableSlackGroup(t *testing.T) {
	testServer := setup()
	defer testServer.Stop()

	LoadSlackMasterData()

	jobInfo := &config.JobInfo{
		SlackGroupObject: slack.UserGroup{ID: "S0615G0KT"},
	}

	DisableSlackGroup(jobInfo)
	assert.NoError(t, jobInfo.Error)
}

func (sd *slackTestData) createListConversationsHandler(w http.ResponseWriter, r *http.Request) {

	channelResponse := struct {
		Channels         []slack.Channel  `json:"channels"`
		ResponseMetaData responseMetadata `json:"response_metadata"`
		slack.SlackResponse
	}{}

	if sd.channels == nil || len(sd.channels) == 0 {
		channelResponse.SlackResponse.Ok = false
		channelResponse.Channels = []slack.Channel{}
		json.NewEncoder(w).Encode(channelResponse)
	}

	channelResponse.SlackResponse.Ok = true
	channelResponse.Channels = sd.channels

	json.NewEncoder(w).Encode(channelResponse)
}

func (sd *slackTestData) createListUsersHandler(w http.ResponseWriter, r *http.Request) {
	usersResponse := struct {
		Users            []slack.User     `json:"members"`
		ResponseMetadata responseMetadata `json:"response_metadata"`
		slack.SlackResponse
	}{}
	usersResponse.Users = sd.users
	usersResponse.Ok = true
	json.NewEncoder(w).Encode(usersResponse)
}

func (sd *slackTestData) createListUserGroupsHandler(w http.ResponseWriter, r *http.Request) {
	userGroupsResponse := struct {
		UserGroups       []slack.UserGroup `json:"usergroups"`
		ResponseMetadata responseMetadata  `json:"response_metadata"`
		slack.SlackResponse
	}{}

	userGroupsResponse.Ok = true
	userGroupsResponse.UserGroups = sd.userGroups
	json.NewEncoder(w).Encode(userGroupsResponse)
}

func (sd *slackTestData) createDisableUserGroupsHandler(w http.ResponseWriter, r *http.Request) {
	ug := r.Form.Get("usergroup")
	userGroupResponse := struct {
		UserGroup slack.UserGroup `json:"usergroup"`
		slack.SlackResponse
	}{}

	userGroupResponse.Ok = false

	for _, g := range sd.userGroups {
		if g.ID == ug {
			userGroupResponse.Ok = true
			userGroupResponse.UserGroup = g
			g.DateDelete = slack.JSONTime(time.Now().Unix())
			g.Users = []string{}
			g.UserCount = 0
		}
	}
	json.NewEncoder(w).Encode(userGroupResponse)
}

func (sd *slackTestData) createUpdateUserGroupsUserHandler(w http.ResponseWriter, r *http.Request) {

	updateUserGroupResponse := struct {
		UserGroup slack.UserGroup `json:"usergroup"`
		slack.SlackResponse
	}{}
	if err := r.ParseForm(); err != nil {
		updateUserGroupResponse.Error = "invalid_arguements"
		updateUserGroupResponse.Ok = false
		json.NewEncoder(w).Encode(updateUserGroupResponse)
		return
	}

	users := strings.Split(r.Form.Get("users"), ",")

	containsUser := func(userGroup slack.UserGroup, username string) bool {
		for _, u := range userGroup.Users {
			if u == username {
				return true
			}
		}
		return false
	}

	for _, g := range sd.userGroups {
		if g.ID == r.Form.Get("usergroup") {
			for _, u := range users {
				if !containsUser(g, u) {
					g.Users = append(g.Users, u)
					g.UserCount = len(g.Users)
				}
			}
			updateUserGroupResponse.UserGroup = g
		}
	}

	updateUserGroupResponse.Ok = true
	json.NewEncoder(w).Encode(updateUserGroupResponse)
}

func createChannelObject(name, id string) slack.Channel {
	return slack.Channel{
		IsChannel: true,
		GroupConversation: slack.GroupConversation{
			Name: name,
			Conversation: slack.Conversation{
				ID: id,
			},
		},
	}
}

func createUserObject(name, id, team_id string) slack.User {
	return slack.User{
		ID:     id,
		Name:   name,
		TeamID: team_id,
	}
}

func createUserGroupObject(id, teamID, name, handle string, users []slack.User) slack.UserGroup {

	usernames := []string{}
	for _, user := range users {
		usernames = append(usernames, user.ID)
	}

	return slack.UserGroup{
		ID:        id,
		TeamID:    teamID,
		Name:      name,
		Handle:    handle,
		UserCount: len(usernames),
		Users:     usernames,
	}
}
