package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/stretchr/testify/assert"

	"github.com/sapcc/pagerduty2slack/internal/config"
)

type expectation struct {
	path     string
	response *http.Response
	query    string
}

type pagerDutyMock struct {
	t            *testing.T
	expectations map[string]*expectation
	DoMock       func(req *http.Request) (*http.Response, error)
}

func (m *pagerDutyMock) Do(req *http.Request) (*http.Response, error) {
	return m.doMock(req)
}

func TestGetUserByEmail(t *testing.T) {
	client, mock := setupPagerDuty(t)

	mock.expectWithQuery("/users", "admin@test.com", usersResponse(user("admin", "0001", true, true)))
	actual, err := client.PdGetUserByEmail("admin@test.com")

	assert.NoError(t, err)
	if assert.NotNil(t, actual) {
		assert.Equal(t, "admin@test.com", actual.Email)
	}
}

func TestFilterUserWithoutPhone(t *testing.T) {
	type filterPhoneTestcase struct {
		users    []pagerduty.User
		expected int
	}

	testcases := []filterPhoneTestcase{
		{
			users: []pagerduty.User{
				user("one", "1", true, true),
				user("two", "2", true, false),
				user("three", "3", true, true),
			},
			expected: 1,
		},
		{
			users: []pagerduty.User{
				user("one", "1", true, true),
				user("two", "2", true, true),
				user("three", "3", true, true),
			},
			expected: 0,
		},
		{
			users: []pagerduty.User{
				user("one", "1", true, false),
				user("two", "2", true, false),
				user("three", "3", true, false),
			},
			expected: 3,
		},
		{
			users: []pagerduty.User{
				user("one", "1", true, false),
				user("two", "2", true, false),
				user("three", "3", false, false),
			},
			expected: 3,
		},
	}

	for _, c := range testcases {
		client, mock := setupPagerDuty(t)
		mock.expect("/users", usersResponse(c.users...))
		actual := client.PdFilterUserWithoutPhone(c.users)
		assert.Equal(t, c.expected, len(actual))
	}
}

func TestGetUser(t *testing.T) {
	client, mock := setupPagerDuty(t)

	type testCase struct {
		apiObject    pagerduty.APIObject
		expectedID   string
		expectedName string
	}

	testCases := []testCase{
		{
			apiObject:    pagerduty.APIObject{ID: "1337", Summary: "Test"},
			expectedID:   "1337",
			expectedName: "BackendResponse",
		},
		{
			apiObject:    pagerduty.APIObject{ID: "1338", Summary: "Test"},
			expectedID:   "1338",
			expectedName: "BackendResponse",
		},
	}

	for _, test := range testCases {
		mock.expect("/users/"+test.expectedID, userResponse(user(test.expectedName, test.expectedID, true, true)))

		actual := client.getUser(test.apiObject)
		assert.Equal(t, test.expectedID, actual.ID)
		assert.Equal(t, test.expectedName, actual.Name)
	}
}

func TestGetPDTeamMembers(t *testing.T) {
	client, mock := setupPagerDuty(t)
	teamIDs := []string{"team_admin", "team_support"}

	mock.expect("/users", usersResponse(
		userWithTeam("admin", "0123", "team_admin", true, true),
		userWithTeam("user01", "0002", "team_support", true, true),
		userWithTeam("user02", "0003", "team_support", true, true),
	))
	mock.expect("/teams/team_admin", teamResult(team("Team Admin", "team_admin")))
	mock.expect("/teams/team_support", teamResult(team("Team Support", "team_support")))

	users, apiObjects, err := client.PdGetTeamMembers(teamIDs)

	assert.NoError(t, err)
	assert.Equal(t, 3, len(users))
	assert.Equal(t, 2, len(apiObjects))
}

func TestGetPDTeamMembersError(t *testing.T) {
	client, mock := setupPagerDuty(t)
	teamIDs := []string{"team_error", "team_support"}

	mock.expect("/users", apiNotFoundError())

	users, apiObjects, err := client.PdGetTeamMembers(teamIDs)

	assert.Error(t, err)
	assert.Nil(t, users)
	assert.Nil(t, apiObjects)
}

func TestListOnCallFinal(t *testing.T) {
	client, mock := setupPagerDuty(t)
	scheduleIDs := []string{"1000", "2000"}
	since := 5 * time.Hour
	until := 5 * time.Hour

	mock.expect("/users/0123", userResponse(user("admin", "0123", true, true)))
	mock.expect("/users/0001", userResponse(user("user01", "0001", true, true)))
	mock.expect("/users/0002", userResponse(user("user02", "0002", true, true)))
	mock.expect("/oncalls", onCallsResult(
		onCall(schedule("Weekly OnCall Rotation", "1000"), policy("Admin", "100"), user("admin", "0123", true, true)),
		onCall(schedule("Daily OnCall Rotation", "2000"), policy("Support", "200"), user("user01", "0001", true, true)),
		onCall(schedule("Daily OnCall Rotation", "2000"), policy("Support", "200"), user("user02", "0002", true, true))))

	mock.expect("/schedules/1000", scheduleResponse(schedule("Weekly OnCallRotation", "1000")))
	mock.expect("/schedules/2000", scheduleResponse(schedule("Daily OnCallRotation", "2000")))

	users, schedules, err := client.pdListOnCallUseFinal(scheduleIDs, since, until)

	assert.NoError(t, err)
	assert.Equal(t, 3, len(users))
	assert.Equal(t, 2, len(schedules))
}

func TestListOnCallUseAllActiveLayers(t *testing.T) {
	client, mock := setupPagerDuty(t)
	scheduleIDs := []string{"3001", "4001"}
	since := 5 * time.Hour
	until := 5 * time.Hour

	mock.expect("/users/0001", userResponse(user("user01", "0001", true, true)))
	mock.expect("/users/0002", userResponse(user("user02", "0002", true, true)))
	mock.expect("/users/0003", userResponse(user("user03", "0003", true, true)))
	mock.expect("/oncalls", onCallsResult(
		onCall(scheduleWithLayer("Schedule With Layers And Override", "3001", user("user02", "0002", true, true)), policy("Layer 2", "301"), user("user01", "0001", true, true)),
		onCall(scheduleWithLayer("Schedule With Layers", "4001", user("user02", "0002", true, true)), policy("Layer Override", "401"), user("user02", "0002", true, true)),
	))
	mock.expect("/schedules/3001", scheduleResponse(scheduleWithLayer("Schedule With Layers And Override", "3001", user("user02", "0002", true, true))))
	mock.expect("/schedules/3001/overrides", overrideResponse(override("0003")))
	mock.expect("/schedules/4001", scheduleResponse(scheduleWithLayer("Schedule With Layers", "4001", user("user02", "0002", true, true))))
	mock.expect("/schedules/4001/overrides", noOverridesResponse())

	users, schedules, err := client.pdListOnCallUseLayers(scheduleIDs, since, until, config.AllActiveLayers)

	assert.NoError(t, err)
	assert.Equal(t, 2, len(users))
	assert.Equal(t, 2, len(schedules))
}

func setupPagerDuty(t *testing.T) (client *PdClient, mock *pagerDutyMock) {
	cfg := config.PagerdutyConfig{AuthToken: "test", APIUser: "test@company.com"}
	c := pagerduty.NewClient("")
	mock = &pagerDutyMock{t: t, expectations: make(map[string]*expectation)}
	c.HTTPClient = mock
	return &PdClient{cfg: &cfg, pagerdutyClient: c}, mock
}

func (m *pagerDutyMock) expect(path string, response *http.Response) {
	m.expectWithQuery(path, "", response)
}

func (m *pagerDutyMock) expectWithQuery(path, query string, response *http.Response) {
	if _, ok := m.expectations[path]; ok {
		m.t.Fatalf("expecation for %s already set", path)
	}
	e := &expectation{path: path, query: query, response: response}
	m.expectations[path] = e
}

func (m *pagerDutyMock) doMock(req *http.Request) (resp *http.Response, err error) {
	pathParts := strings.Split(req.URL.Path, "/")
	path := strings.Join(pathParts, "/")
	if exp, ok := m.expectations[path]; ok {
		return exp.response, nil
	}
	m.t.Fatalf("expectation for '%s' is missing", path)
	return createResponse(http.StatusNotImplemented, nil), nil
}

func createResponse(statusCode int, result any) *http.Response {
	r := []byte{}
	b := bytes.NewBuffer(r)
	e := json.NewEncoder(b).Encode(result)
	if e != nil {
		return &http.Response{
			Body:       http.NoBody,
			StatusCode: http.StatusInternalServerError,
		}
	}

	return &http.Response{
		Body:       io.NopCloser(b),
		StatusCode: statusCode}
}

func userResponse(user pagerduty.User) *http.Response {
	result := make(map[string]pagerduty.User)
	result["user"] = user
	return createResponse(http.StatusOK, result)
}

func usersResponse(users ...pagerduty.User) *http.Response {
	return createResponse(http.StatusOK, pagerduty.ListUsersResponse{
		Users: users,
	})
}

func user(name, id string, email, phone bool) pagerduty.User {
	return userWithTeam(name, id, "default", email, phone)
}

func userWithTeam(name, id, teamid string, email, phone bool) pagerduty.User {
	user := pagerduty.User{
		Name:      name,
		Email:     fmt.Sprintf("%s@test.com", strings.ToLower(name)),
		APIObject: pagerduty.APIObject{ID: id},
		Teams: []pagerduty.Team{
			{
				APIObject: pagerduty.APIObject{
					ID: teamid,
				},
			},
		},
	}
	if email {
		user.ContactMethods = append(user.ContactMethods, pagerduty.ContactMethod{Type: "email_contact_method_reference"})
	}

	if phone {
		user.ContactMethods = append(user.ContactMethods, pagerduty.ContactMethod{Type: "phone_contact_method_reference"})
	}
	return user
}

func teamResult(team pagerduty.Team) *http.Response {
	return createResponse(http.StatusOK, map[string]pagerduty.Team{"team": team})
}

func team(name, id string) pagerduty.Team {
	return pagerduty.Team{Name: name, APIObject: pagerduty.APIObject{ID: id}}
}

func onCallsResult(oncalls ...pagerduty.OnCall) *http.Response {
	return createResponse(http.StatusOK, pagerduty.ListOnCallsResponse{
		OnCalls: oncalls,
	})
}

func onCall(schedule pagerduty.Schedule, policy pagerduty.EscalationPolicy, user pagerduty.User) pagerduty.OnCall {
	return pagerduty.OnCall{Schedule: schedule, EscalationPolicy: policy, User: user}
}

func scheduleResponse(schedule pagerduty.Schedule) *http.Response {
	return createResponse(http.StatusOK, map[string]pagerduty.Schedule{
		"schedule": schedule,
	})
}

func schedule(name, id string) pagerduty.Schedule {
	return pagerduty.Schedule{APIObject: pagerduty.APIObject{ID: id}, Name: name}
}

func scheduleWithLayer(name, id string, users ...pagerduty.User) pagerduty.Schedule {
	var entries []pagerduty.RenderedScheduleEntry
	for _, u := range users {
		entries = append(entries, pagerduty.RenderedScheduleEntry{User: u.APIObject})
	}
	return pagerduty.Schedule{APIObject: pagerduty.APIObject{ID: id}, Name: name, ScheduleLayers: []pagerduty.ScheduleLayer{{RenderedScheduleEntries: entries}}}
}

func overrideResponse(override pagerduty.Override) *http.Response {
	return createResponse(http.StatusOK, pagerduty.ListOverridesResponse{Overrides: []pagerduty.Override{override}})
}

func noOverridesResponse() *http.Response {
	return createResponse(http.StatusOK, pagerduty.ListOverridesResponse{Overrides: []pagerduty.Override{}})
}

func override(id string) pagerduty.Override {
	return pagerduty.Override{
		User: pagerduty.APIObject{ID: id},
	}
}

func policy(name, id string) pagerduty.EscalationPolicy {
	return pagerduty.EscalationPolicy{APIObject: pagerduty.APIObject{ID: id}, Name: name}
}

func apiNotFoundError() *http.Response {
	return createResponse(http.StatusNotFound, pagerduty.APIError{
		StatusCode: http.StatusNotFound,
		APIError:   pagerduty.NullAPIErrorObject{Valid: true, ErrorObject: pagerduty.APIErrorObject{Code: 2001, Message: "Not Found"}}})
}
