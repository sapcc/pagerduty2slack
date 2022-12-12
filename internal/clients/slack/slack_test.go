package slack

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slacktest"
	"github.com/stretchr/testify/assert"

	"github.com/sapcc/pagerduty2slack/internal/config"
)

type slackTestData struct {
	userGroups []slack.UserGroup
	channels   []slack.Channel
	users      []slack.User
}

type responseMetadata struct {
	NextCursor string `json:"next_cursor"`
}

func setup(t *testing.T) *slacktest.Server {
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

	slackInfoChannelName = "general"

	cfg := config.SlackConfig{
		UserSecurityToken: "TEST_TOKEN",
		BotSecurityToken:  "TEST_TOKEN",
	}
	var err error
	apiURLOption := slack.OptionAPIURL(testServer.GetAPIURL())
	defaultSlackClientBot, err = NewSlackClient(cfg, SlackClientTypeBot, apiURLOption)
	if err != nil {
		t.Fatalf("failed setting up test server: %s", err.Error())
	}
	defaultSlackClientUser, err = NewSlackClient(cfg, SlackClientTypeUser, apiURLOption)
	if err != nil {
		t.Fatalf("failed setting up test server: %s", err.Error())
	}
	return testServer
}

func TestGetChannels(t *testing.T) {
	testServer := setup(t)

	defer testServer.Stop()
	channels, err := GetChannels()
	if assert.NoError(t, err) {
		assert.NotEmpty(t, channels)
	}
}

func TestGetSlackGroup(t *testing.T) {
	testServer := setup(t)
	defer testServer.Stop()
	if err := LoadMasterData(); err != nil {
		t.Fatalf("unexpected err loading masterdata: %s", err.Error())
	}

	group, err := GetSlackGroup("admins")
	if assert.NoError(t, err) {
		assert.Equal(t, "Team Admins", group.Name)
	}
}

func TestLoadSlackMasterData(t *testing.T) {
	testServer := setup(t)
	defer testServer.Stop()

	assert.NoError(t, LoadMasterData())
	assert.NotEmpty(t, slackChannels)
	assert.NotEmpty(t, slackUserList)
	assert.NotEmpty(t, slackGrps)
}

func TestGetSlackUser(t *testing.T) {
	pdUsers := []pagerduty.User{{Email: "spengler@ghostbusters.example.com"}}
	slackUserList = []slack.User{
		{Profile: slack.UserProfile{Email: "spengler@ghostbusters.example.com"}},
		{Profile: slack.UserProfile{Email: "max@mustermann.example.com"}},
	}

	actualUsers, err := MatchPDUsers(pdUsers)

	if assert.NoError(t, err) {
		assert.Len(t, actualUsers, 1)
		assert.Equal(t, "spengler@ghostbusters.example.com", actualUsers[0].Profile.Email)
	}
}

func TestSetSlackUserGroup(t *testing.T) {
	testServer := setup(t)
	defer testServer.Stop()

	if err := LoadMasterData(); err != nil {
		t.Fatalf("unexpected err loading masterdata: %s", err.Error())
	}

	jobInfo := &config.JobInfo{JobType: config.PdTeamSync,
		Cfg: config.Config{
			Jobs: config.JobsConfig{
				TeamSync: []config.PagerdutyTeamToSlackGroup{
					{
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
	slackUsers := []slack.User{{ID: "W012A3CDE"}}
	noChange, err := AddToGroup(jobInfo, slackUsers)

	assert.NoError(t, err)
	assert.False(t, noChange)
}

func TestSetSlackUserGroupNoChange(t *testing.T) {
	testServer := setup(t)
	defer testServer.Stop()

	if err := LoadMasterData(); err != nil {
		t.Fatalf("unexpected err loading masterdata: %s", err.Error())
	}

	jobInfo := &config.JobInfo{JobType: config.PdTeamSync,
		Cfg: config.Config{
			Jobs: config.JobsConfig{
				TeamSync: []config.PagerdutyTeamToSlackGroup{
					{
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
	slackUsers := []slack.User{
		{ID: "W012A3CDE"},
		{ID: "W07QCRPA4"},
	}
	noChange, err := AddToGroup(jobInfo, slackUsers)

	assert.NoError(t, err)
	assert.True(t, noChange)
}

func TestDisableSlackGroup(t *testing.T) {
	testServer := setup(t)
	defer testServer.Stop()

	if err := LoadMasterData(); err != nil {
		t.Fatalf("unexpected err loading masterdata: %s", err.Error())
	}

	jobInfo := &config.JobInfo{
		SlackGroupObject: slack.UserGroup{ID: "S0615G0KT"},
	}

	DisableGroup(jobInfo)
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
		if err := json.NewEncoder(w).Encode(channelResponse); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}

	channelResponse.SlackResponse.Ok = true
	channelResponse.Channels = sd.channels

	if err := json.NewEncoder(w).Encode(channelResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (sd *slackTestData) createListUsersHandler(w http.ResponseWriter, r *http.Request) {
	usersResponse := struct {
		Users            []slack.User     `json:"members"`
		ResponseMetadata responseMetadata `json:"response_metadata"`
		slack.SlackResponse
	}{}
	usersResponse.Users = sd.users
	usersResponse.Ok = true
	if err := json.NewEncoder(w).Encode(usersResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (sd *slackTestData) createListUserGroupsHandler(w http.ResponseWriter, r *http.Request) {
	userGroupsResponse := struct {
		UserGroups       []slack.UserGroup `json:"usergroups"`
		ResponseMetadata responseMetadata  `json:"response_metadata"`
		slack.SlackResponse
	}{}

	userGroupsResponse.Ok = true
	userGroupsResponse.UserGroups = sd.userGroups
	if err := json.NewEncoder(w).Encode(userGroupsResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
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
	if err := json.NewEncoder(w).Encode(userGroupResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (sd *slackTestData) createUpdateUserGroupsUserHandler(w http.ResponseWriter, r *http.Request) {
	updateUserGroupResponse := struct {
		UserGroup slack.UserGroup `json:"usergroup"`
		slack.SlackResponse
	}{}
	if err := r.ParseForm(); err != nil {
		updateUserGroupResponse.Error = "invalid_arguments"
		updateUserGroupResponse.Ok = false
		if err := json.NewEncoder(w).Encode(updateUserGroupResponse); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
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
	if err := json.NewEncoder(w).Encode(updateUserGroupResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
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

func createUserObject(name, id, teamID string) slack.User {
	return slack.User{
		ID:     id,
		Name:   name,
		TeamID: teamID,
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
