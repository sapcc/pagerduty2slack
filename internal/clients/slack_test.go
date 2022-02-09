package clients

import (
	"net/http"
	"testing"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/sapcc/pagerduty2slack/internal/config"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slacktest"
	"github.com/stretchr/testify/assert"
)

func setup() *slacktest.Server {
	testServer := slacktest.NewTestServer()
	go testServer.Start()

	testServer.Handle("/conversations.list", createListConversationsHandler)
	testServer.Handle("/users.list", createListUsersHandler)
	testServer.Handle("/usergroups.list", createListUserGroupsHandler)
	testServer.Handle("/usergroups.users.update", createUpdateUserGroupsUserHandler)

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

	LoadSlackMasterData()
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
		SlackGroupObject: slack.UserGroup{
			Users: []string{"W012A3CDE"},
		},
	}
	slackUsers := []slack.User{slack.User{ID: "W012A3CDE"}}
	actual := SetSlackGroupUser(jobInfo, slackUsers)

	assert.True(t, actual)

}

func createListConversationsHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(defaultChannelListJSON))
}

func createListUsersHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(defaultUserListJSON))
}

func createListUserGroupsHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(defaultUserGroupListJSON))
}

func createUpdateUserGroupsUserHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(defaultUpdateUserGroupsUserJSON))
}

var defaultChannelListJSON = `
{
	"ok": true,
	"channels": [
		{
			"id": "C012AB3CD",
			"name": "general",
			"is_channel": true,
			"is_group": false,
			"is_im": false,
			"created": 1449252889,
			"creator": "U012A3CDE",
			"is_archived": false,
			"is_general": true,
			"unlinked": 0,
			"name_normalized": "general",
			"is_shared": false,
			"is_ext_shared": false,
			"is_org_shared": false,
			"pending_shared": [],
			"is_pending_ext_shared": false,
			"is_member": true,
			"is_private": false,
			"is_mpim": false,
			"topic": {
				"value": "Company-wide announcements and work-based matters",
				"creator": "",
				"last_set": 0
			},
			"purpose": {
				"value": "This channel is for team-wide communication and announcements. All team members are in this channel.",
				"creator": "",
				"last_set": 0
			},
			"previous_names": [],
			"num_members": 4
		},
		{
			"id": "C061EG9T2",
			"name": "random",
			"is_channel": true,
			"is_group": false,
			"is_im": false,
			"created": 1449252889,
			"creator": "U061F7AUR",
			"is_archived": false,
			"is_general": false,
			"unlinked": 0,
			"name_normalized": "random",
			"is_shared": false,
			"is_ext_shared": false,
			"is_org_shared": false,
			"pending_shared": [],
			"is_pending_ext_shared": false,
			"is_member": true,
			"is_private": false,
			"is_mpim": false,
			"topic": {
				"value": "Non-work banter and water cooler conversation",
				"creator": "",
				"last_set": 0
			},
			"purpose": {
				"value": "A place for non-work-related flimflam, faffing, hodge-podge or jibber-jabber you'd prefer to keep out of more focused work-related channels.",
				"creator": "",
				"last_set": 0
			},
			"previous_names": [],
			"num_members": 4
		}
    ],
    "response_metadata": {
			"next_cursor": "dGVhbTpDMDYxRkE1UEI="
    }
		}`

var defaultUserListJSON = `
{
    "ok": true,
    "members": [
        {
            "id": "W012A3CDE",
            "team_id": "T012AB3C4",
            "name": "spengler",
            "deleted": false,
            "color": "9f69e7",
            "real_name": "spengler",
            "tz": "America/Los_Angeles",
            "tz_label": "Pacific Daylight Time",
            "tz_offset": -25200,
            "profile": {
                "avatar_hash": "ge3b51ca72de",
                "status_text": "Print is dead",
                "status_emoji": ":books:",
                "real_name": "Egon Spengler",
                "display_name": "spengler",
                "real_name_normalized": "Egon Spengler",
                "display_name_normalized": "spengler",
                "email": "spengler@ghostbusters.example.com",
                "image_24": "https://.../avatar/e3b51ca72dee4ef87916ae2b9240df50.jpg",
                "image_32": "https://.../avatar/e3b51ca72dee4ef87916ae2b9240df50.jpg",
                "image_48": "https://.../avatar/e3b51ca72dee4ef87916ae2b9240df50.jpg",
                "image_72": "https://.../avatar/e3b51ca72dee4ef87916ae2b9240df50.jpg",
                "image_192": "https://.../avatar/e3b51ca72dee4ef87916ae2b9240df50.jpg",
                "image_512": "https://.../avatar/e3b51ca72dee4ef87916ae2b9240df50.jpg",
                "team": "T012AB3C4"
            },
            "is_admin": true,
            "is_owner": false,
            "is_primary_owner": false,
            "is_restricted": false,
            "is_ultra_restricted": false,
            "is_bot": false,
            "updated": 1502138686,
            "is_app_user": false,
            "has_2fa": false
        },
        {
            "id": "W07QCRPA4",
            "team_id": "T0G9PQBBK",
            "name": "glinda",
            "deleted": false,
            "color": "9f69e7",
            "real_name": "Glinda Southgood",
            "tz": "America/Los_Angeles",
            "tz_label": "Pacific Daylight Time",
            "tz_offset": -25200,
            "profile": {
                "avatar_hash": "8fbdd10b41c6",
                "image_24": "https://a.slack-edge.com...png",
                "image_32": "https://a.slack-edge.com...png",
                "image_48": "https://a.slack-edge.com...png",
                "image_72": "https://a.slack-edge.com...png",
                "image_192": "https://a.slack-edge.com...png",
                "image_512": "https://a.slack-edge.com...png",
                "image_1024": "https://a.slack-edge.com...png",
                "image_original": "https://a.slack-edge.com...png",
                "first_name": "Glinda",
                "last_name": "Southgood",
                "title": "Glinda the Good",
                "phone": "",
                "skype": "",
                "real_name": "Glinda Southgood",
                "real_name_normalized": "Glinda Southgood",
                "display_name": "Glinda the Fairly Good",
                "display_name_normalized": "Glinda the Fairly Good",
                "email": "glenda@south.oz.coven"
            },
            "is_admin": true,
            "is_owner": false,
            "is_primary_owner": false,
            "is_restricted": false,
            "is_ultra_restricted": false,
            "is_bot": false,
            "updated": 1480527098,
            "has_2fa": false
        }
    ],
    "cache_ts": 1498777272,
    "response_metadata": {
    }
}`

var defaultUserGroupListJSON = `{
				"ok": true,
				"usergroups": [
						{
								"id": "S0614TZR7",
								"team_id": "T060RNRCH",
								"is_usergroup": true,
								"name": "Team Admins",
								"description": "A group of all Administrators on your team.",
								"handle": "admins",
								"is_external": false,
								"date_create": 1446598059,
								"date_update": 1446670362,
								"date_delete": 0,
								"auto_type": "admin",
								"created_by": "USLACKBOT",
								"updated_by": "U060RNRCZ",
								"deleted_by": null,
								"prefs": {
										"channels": [],
										"groups": []
								},
								"user_count": 2
						},
						{
								"id": "S06158AV7",
								"team_id": "T060RNRCH",
								"is_usergroup": true,
								"name": "Team Owners",
								"description": "A group of all Owners on your team.",
								"handle": "owners",
								"is_external": false,
								"date_create": 1446678371,
								"date_update": 1446678371,
								"date_delete": 0,
								"auto_type": "owner",
								"created_by": "USLACKBOT",
								"updated_by": "USLACKBOT",
								"deleted_by": null,
								"prefs": {
										"channels": [],
										"groups": []
								},
								"user_count": 1
						},
						{
								"id": "S0615G0KT",
								"team_id": "T060RNRCH",
								"is_usergroup": true,
								"name": "Marketing Team",
								"description": "Marketing gurus, PR experts and product advocates.",
								"handle": "marketing-team",
								"is_external": false,
								"date_create": 1446746793,
								"date_update": 1446747767,
								"date_delete": 1446748865,
								"auto_type": null,
								"created_by": "U060RNRCZ",
								"updated_by": "U060RNRCZ",
								"deleted_by": null,
								"prefs": {
										"channels": [],
										"groups": []
								},
								"user_count": 0
						}
				]
		}
		`

var defaultUpdateUserGroupsUserJSON = `
{
    "ok": true,
    "usergroup": {
        "id": "S0616NG6M",
        "team_id": "T060R4BHN",
        "is_usergroup": true,
        "name": "Marketing Team",
        "description": "Marketing gurus, PR experts and product advocates.",
        "handle": "marketing-team",
        "is_external": false,
        "date_create": 1447096577,
        "date_update": 1447102109,
        "date_delete": 0,
        "auto_type": null,
        "created_by": "U060R4BJ4",
        "updated_by": "U060R4BJ4",
        "deleted_by": null,
        "prefs": {
            "channels": [],
            "groups": []
        },
        "users": [
            "U060R4BJ4",
            "U060RNRCZ"
        ],
        "user_count": 1
    }
}`
