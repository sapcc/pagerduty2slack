package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/sapcc/pagerduty2slack/internal/config"
	"github.com/stretchr/testify/assert"
)

type pagerDutyMock struct {
	DoMock func(req *http.Request) (*http.Response, error)
}

func (m *pagerDutyMock) Do(req *http.Request) (*http.Response, error) {
	return doMock(req)
}

func TestGetUserByEmail(t *testing.T) {
	mock := setupPagerDuty()
	actual, err := mock.PdGetUserByEmail("admin@test.com")

	assert.NoError(t, err)
	assert.Equal(t, "admin@test.com", actual.Email)
}

func TestFilterUserWithoutPhone(t *testing.T) {

	mock := setupPagerDuty()

	type filterPhoneTestcase struct {
		users    []pagerduty.User
		expected int
	}

	testcases := []filterPhoneTestcase{
		filterPhoneTestcase{
			users: []pagerduty.User{
				createMockUser("one", "1", true, true),
				createMockUser("two", "2", true, false),
				createMockUser("three", "3", true, true),
			},
			expected: 1,
		},
		filterPhoneTestcase{
			users: []pagerduty.User{
				createMockUser("one", "1", true, true),
				createMockUser("two", "2", true, true),
				createMockUser("three", "3", true, true),
			},
			expected: 0,
		},
		filterPhoneTestcase{
			users: []pagerduty.User{
				createMockUser("one", "1", true, false),
				createMockUser("two", "2", true, false),
				createMockUser("three", "3", true, false),
			},
			expected: 3,
		},
	}

	for _, c := range testcases {
		actual := mock.PdFilterUserWithoutPhone(c.users)
		assert.Equal(t, c.expected, len(actual))
	}
}

func TestGetUser(t *testing.T) {
	mock := setupPagerDuty()

	type testCase struct {
		apiObject    pagerduty.APIObject
		expectedID   string
		expectedName string
	}

	testCases := []testCase{
		testCase{
			apiObject:    pagerduty.APIObject{ID: "1337", Summary: "Test"},
			expectedID:   "1337",
			expectedName: "BackendResponse",
		},
		testCase{
			apiObject:    pagerduty.APIObject{ID: "1338", Summary: "Test"},
			expectedID:   "1338",
			expectedName: "Test",
		},
	}

	for _, test := range testCases {
		actual := mock.getUser(test.apiObject)
		assert.Equal(t, test.expectedID, actual.ID)
		assert.Equal(t, test.expectedName, actual.Name)
	}
}

func TestGetPDTeamMembers(t *testing.T) {
	mock := setupPagerDuty()
	teamIDs := []string{"team_admin", "team_support"}
	users, apiObjects, err := mock.PdGetTeamMembers(teamIDs)

	assert.NoError(t, err)
	assert.Equal(t, 3, len(users))
	assert.Equal(t, 2, len(apiObjects))
}

func TestGetPDTeamMembersError(t *testing.T) {
	mock := setupPagerDuty()
	teamIDs := []string{"team_error", "team_support"}
	users, apiObjects, err := mock.PdGetTeamMembers(teamIDs)

	assert.Error(t, err)
	assert.Nil(t, users)
	assert.Nil(t, apiObjects)
}

func TestListOnCallFinal(t *testing.T) {
	mock := setupPagerDuty()
	scheduleIDs := []string{"1000", "2000"}
	sinceOffset, _ := time.ParseDuration("-5h")
	untilOffset, _ := time.ParseDuration("+5h")
	users, apiObjects, err := mock.pdListOnCallUseFinal(scheduleIDs, sinceOffset, untilOffset)

	assert.NoError(t, err)
	assert.Equal(t, 3, len(users))
	assert.Equal(t, 3, len(apiObjects))
}

func setupPagerDuty() *PdClient {
	cfg := config.PagerdutyConfig{AuthToken: "test", ApiUser: "test@company.com"}
	pdc := pagerduty.Client{HTTPClient: &pagerDutyMock{}}
	return &PdClient{cfg: &cfg, pagerdutyClient: &pdc}

}

func doMock(req *http.Request) (resp *http.Response, err error) {
	pathParts := strings.Split(req.URL.Path, "/")
	route := pathParts[1]
	var id string
	if len(pathParts) == 3 {
		id = pathParts[2]
	}

	switch route {
	case "users":
		if id == "" { //ListUser request
			result := pagerduty.ListUsersResponse{}
			if req.URL.Query().Get("query") == "admin@test.com" {
				result.Users = append(result.Users, createMockUser("Admin", "0123", true, true))
			} else {
				result.Users = createMockUsersList()
			}
			return createResponse(200, result), nil
		} else {
			if id == "1337" {
				result := make(map[string]pagerduty.User)
				result["user"] = pagerduty.User{
					Name:      "BackendResponse",
					Email:     "test@test.com",
					Type:      "user",
					APIObject: pagerduty.APIObject{ID: "1337"},
				}
				return createResponse(200, result), nil
			} else {
				result := pagerduty.APIError{
					StatusCode: 404,
					APIError:   pagerduty.NullAPIErrorObject{Valid: true, ErrorObject: pagerduty.APIErrorObject{Code: 2001, Message: "Not Found"}},
				}
				return createResponse(404, result), nil
			}
		}
	case "teams":
		if id == "team_error" {
			result := pagerduty.APIError{
				StatusCode: 404,
				APIError:   pagerduty.NullAPIErrorObject{Valid: true, ErrorObject: pagerduty.APIErrorObject{Code: 6001, Message: "Team Not Found"}},
			}
			return createResponse(404, result), nil
		} else {
			result := make(map[string]pagerduty.Team)
			result["team"] = createMockTeam(pathParts[2])
			return createResponse(200, result), nil
		}
	case "oncalls":
		return createResponse(200, createMockOnCalls()), nil
	}
	return
}

func createResponse(statusCode int, result any) *http.Response {
	r := []byte{}
	b := bytes.NewBuffer(r)
	json.NewEncoder(b).Encode(result)

	return &http.Response{
		Body:       ioutil.NopCloser(b),
		StatusCode: statusCode}
}

func createMockUser(name, id string, email, phone bool) pagerduty.User {
	return createMockUserWithTeam(name, id, "default", email, phone)
}

func createMockUserWithTeam(name, id, teamid string, email, phone bool) pagerduty.User {
	user := pagerduty.User{Name: name, Email: fmt.Sprintf("%s@test.com", strings.ToLower(name)), APIObject: pagerduty.APIObject{ID: id}, Teams: []pagerduty.Team{pagerduty.Team{APIObject: pagerduty.APIObject{ID: teamid}}}}
	if email {
		user.ContactMethods = append(user.ContactMethods, pagerduty.ContactMethod{Type: "email_contact_method_reference"})
	}

	if phone {
		user.ContactMethods = append(user.ContactMethods, pagerduty.ContactMethod{Type: "phone_contact_method_reference"})
	}
	return user
}

func createMockUsersList() []pagerduty.User {
	users := []pagerduty.User{}
	users = append(users, createMockUserWithTeam("Admin", "0123", "team_admin", true, true))
	users = append(users, createMockUserWithTeam("User1", "001", "team_support", true, true))
	users = append(users, createMockUserWithTeam("User2", "002", "team_support", true, true))
	return users
}

func createMockTeam(id string) pagerduty.Team {
	switch id {
	case "team_admin":
		return pagerduty.Team{Name: "Admin Team", APIObject: pagerduty.APIObject{ID: id}}
	case "team_support":
		return pagerduty.Team{Name: "Admin Team", APIObject: pagerduty.APIObject{ID: id}}
	}
	return pagerduty.Team{}
}

func createMockOnCalls() pagerduty.ListOnCallsResponse {
	oncalls := []pagerduty.OnCall{}

	oncalls = append(oncalls, pagerduty.OnCall{
		User:             pagerduty.User{APIObject: pagerduty.APIObject{ID: "0123"}},
		EscalationLevel:  1,
		EscalationPolicy: pagerduty.EscalationPolicy{Name: "Admin", APIObject: pagerduty.APIObject{ID: "100"}},
		Schedule:         pagerduty.Schedule{Name: "Weekly OnCall Rotation", APIObject: pagerduty.APIObject{ID: "1000"}}})
	oncalls = append(oncalls, pagerduty.OnCall{
		User:             pagerduty.User{APIObject: pagerduty.APIObject{ID: "001"}},
		EscalationLevel:  2,
		EscalationPolicy: pagerduty.EscalationPolicy{Name: "Support", APIObject: pagerduty.APIObject{ID: "200"}},
		Schedule:         pagerduty.Schedule{Name: "Daily OnCall Rotation", APIObject: pagerduty.APIObject{ID: "2000"}}})
	oncalls = append(oncalls, pagerduty.OnCall{
		User:             pagerduty.User{APIObject: pagerduty.APIObject{ID: "002"}},
		EscalationLevel:  2,
		EscalationPolicy: pagerduty.EscalationPolicy{Name: "Support", APIObject: pagerduty.APIObject{ID: "200"}}, Schedule: pagerduty.Schedule{Name: "Daily OnCall Rotation", APIObject: pagerduty.APIObject{ID: "2000"}}})
	return pagerduty.ListOnCallsResponse{OnCalls: oncalls}
}
