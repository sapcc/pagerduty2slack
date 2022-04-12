package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

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
				createPDUser("one", "1", true, true),
				createPDUser("two", "2", true, false),
				createPDUser("three", "3", true, true),
			},
			expected: 1,
		},
		filterPhoneTestcase{
			users: []pagerduty.User{
				createPDUser("one", "1", true, true),
				createPDUser("two", "2", true, true),
				createPDUser("three", "3", true, true),
			},
			expected: 0,
		},
		filterPhoneTestcase{
			users: []pagerduty.User{
				createPDUser("one", "1", true, false),
				createPDUser("two", "2", true, false),
				createPDUser("three", "3", true, false),
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

func setupPagerDuty() *PdClient {
	cfg := config.PagerdutyConfig{AuthToken: "test", ApiUser: "test@company.com"}
	pdc := pagerduty.Client{HTTPClient: &pagerDutyMock{}}
	return &PdClient{cfg: &cfg, pagerdutyClient: &pdc}

}

func doMock(req *http.Request) (resp *http.Response, err error) {
	pathBlocks := strings.Split(req.URL.Path, "/")
	switch pathBlocks[1] {
	case "users":
		if len(pathBlocks) == 2 { //ListUser request
			result := pagerduty.ListUsersResponse{}
			if req.URL.Query().Get("query") == "admin@test.com" {
				result.Users = append(result.Users, createPDUser("Admin", "0123", true, true))
			} else {
				result.Users = createUsersList()
			}
			return createResponse(200, result), nil
		}

		if len(pathBlocks) == 3 {
			if pathBlocks[2] == "1337" {
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
		return
	case "teams":
		result := make(map[string]pagerduty.Team)
		result["team"] = createTeam(pathBlocks[2])
		return createResponse(200, result), nil
	}
	return
}

func createResponse(statusCode int, result any) *http.Response {
	r := []byte{}
	b := bytes.NewBuffer(r)
	json.NewEncoder(b).Encode(result)

	return &http.Response{
		Body:       ioutil.NopCloser(b),
		StatusCode: 200}
}

func createPDUser(name, id string, email, phone bool) pagerduty.User {
	return createPDUserWithTeam(name, id, "default", email, phone)
}

func createPDUserWithTeam(name, id, teamid string, email, phone bool) pagerduty.User {
	user := pagerduty.User{Name: name, Email: fmt.Sprintf("%s@test.com", strings.ToLower(name)), APIObject: pagerduty.APIObject{ID: id}, Teams: []pagerduty.Team{pagerduty.Team{APIObject: pagerduty.APIObject{ID: teamid}}}}
	if email {
		user.ContactMethods = append(user.ContactMethods, pagerduty.ContactMethod{Type: "email_contact_method_reference"})
	}

	if phone {
		user.ContactMethods = append(user.ContactMethods, pagerduty.ContactMethod{Type: "phone_contact_method_reference"})
	}
	return user
}

func createUsersList() []pagerduty.User {
	users := []pagerduty.User{}

	users = append(users, createPDUserWithTeam("Admin", "0123", "team_admin", true, true))
	users = append(users, createPDUserWithTeam("User1", "001", "team_support", true, true))
	users = append(users, createPDUserWithTeam("User2", "002", "team_support", true, true))
	return users
}

func createTeam(id string) pagerduty.Team {
	switch id {
	case "team_admin":
		return pagerduty.Team{Name: "Admin Team", APIObject: pagerduty.APIObject{ID: id}}
	case "team_support":
		return pagerduty.Team{Name: "Admin Team", APIObject: pagerduty.APIObject{ID: id}}
	}
	return pagerduty.Team{}
}
