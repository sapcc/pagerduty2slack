package pagerduty

import (
	"context"
	"fmt"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/sapcc/pulsar/pkg/util"
	log "github.com/sirupsen/logrus"

	"github.com/sapcc/pagerduty2slack/internal/config"
)

type offsetInHours = time.Duration

// PdClient wraps the pagerduty client.
type PdClient struct {
	cfg             *config.PagerdutyConfig
	pagerdutyClient *pagerduty.Client
	apiUserInstance *pagerduty.User
}

// NewClient returns a new PagerdutyClient or an error.
func NewClient(cfg *config.PagerdutyConfig) (*PdClient, error) {
	pagerdutyClient := pagerduty.NewClient(cfg.AuthToken)
	if pagerdutyClient == nil {
		return nil, fmt.Errorf("pagerduty: failed to initialize client")
	}

	c := &PdClient{
		cfg:             cfg,
		pagerdutyClient: pagerdutyClient,
	}

	defaultUser, err := c.PdGetUserByEmail(cfg.APIUser)
	if err != nil {
		return nil, fmt.Errorf("pagerduty: getting default user by email '%s' failed: %w", cfg.APIUser, err)
	}
	c.apiUserInstance = defaultUser

	return c, nil
}

// PdGetUserByEmail returns the pagerduty user for the given email or an error.
func (c *PdClient) PdGetUserByEmail(email string) (*pagerduty.User, error) {
	userList, err := c.pagerdutyClient.ListUsersWithContext(context.TODO(), pagerduty.ListUsersOptions{Query: email})
	if err != nil {
		return nil, err
	}
	// can this be more than 1 user?
	for _, user := range userList.Users {
		if user.Email == email {
			return &user, nil
		}
	}

	return nil, fmt.Errorf("user with email '%s' not found", email)
}

// WithoutPhone gives all User without a phone number set
func (c *PdClient) WithoutPhone(users []pagerduty.User) []pagerduty.User {
	noPhoneUsers := []pagerduty.User{}

	for _, user := range users {
		hasPhone := false
		for _, c := range user.ContactMethods {
			if c.Type == "phone_contact_method_reference" {
				hasPhone = true
			}
		}
		if !hasPhone {
			noPhoneUsers = append(noPhoneUsers, user)
		}
	}
	return noPhoneUsers
}

// ListOnCallUsers returns the OnCall users being on shift now
func (c *PdClient) ListOnCallUsers(scheduleIDs []string, since, until offsetInHours, layerSyncStyle config.SyncStyle) ([]pagerduty.User, []pagerduty.APIObject, error) {
	if layerSyncStyle == config.FinalLayer {
		return c.pdListOnCallUseFinal(scheduleIDs, since, until)
	} else {
		return c.pdListOnCallUseLayers(scheduleIDs, since, until, layerSyncStyle)
	}
}

func (c *PdClient) pdListOnCallUseFinal(scheduleIDs []string, since, until offsetInHours) (users []pagerduty.User, schedules []pagerduty.APIObject, err error) {
	onCallOpts := pagerduty.ListOnCallOptions{
		ScheduleIDs: scheduleIDs,
		Since:       util.TimestampToString(time.Now().Add(-since)),
		Until:       util.TimestampToString(time.Now().Add(until)),
		//Includes: []string{"users","schedules"}, // doesn't work - workaround sub request
	}
	resp, err := c.pagerdutyClient.ListOnCallsWithContext(context.TODO(), onCallOpts)
	if err != nil {
		return nil, nil, err
	}
	users = c.listOnCallUsers(resp.OnCalls)
	schedules, err = c.listOnCallSchedules(scheduleIDs, since, until)
	if err != nil {
		return nil, nil, err
	}
	return users, schedules, nil
}

func (c *PdClient) pdListOnCallUseLayers(scheduleIDs []string, since, until offsetInHours, layerSyncStyle config.SyncStyle) (users []pagerduty.User, schedules []pagerduty.APIObject,
	err error) {
	// query options for schedule and override request (we needed since the api doesn't deliver the override info, beside api docu said it should)
	scheduleOpts := pagerduty.GetScheduleOptions{
		TimeZone: "UTC",
		Since:    util.TimestampToString(time.Now().UTC().Add(-since)),
		Until:    util.TimestampToString(time.Now().UTC().Add(until)),
	}
	overrideOpts := pagerduty.ListOverridesOptions{
		Since: util.TimestampToString(time.Now().UTC().Add(-since)),
		Until: util.TimestampToString(time.Now().UTC().Add(until)),
	}

	uniqueUsers := make(map[string]struct{})
	// get schedule objects
	for _, id := range scheduleIDs {
		schedule, err := c.pagerdutyClient.GetScheduleWithContext(context.TODO(), id, scheduleOpts)
		if schedule == nil || err != nil {
			return nil, schedules, err
		}
		schedules = append(schedules, schedule.APIObject)

		// get overrides (since we can't trust the info in schedule object, we have to request separately until API is fixed
		overrides, err := c.pagerdutyClient.ListOverridesWithContext(context.TODO(), id, overrideOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("pagerduty: failed listing overrides: %w", err)
		}
		if overrides != nil {
			// add override layer if exist
			if len(overrides.Overrides) > 0 {
				for _, o := range overrides.Overrides {
					if _, ok := uniqueUsers[o.User.ID]; !ok {
						uniqueUsers[o.User.ID] = struct{}{}
						users = append(users, c.getUser(o.User))
					}
				}
				log.Debugf("pagerduty: handled overrides for  schedule%s[%s]", schedule.Name, schedule.ID)
				// if exist and we do not need the other layers - jump to next schedule
				if layerSyncStyle == config.OverridesOnlyIfThere {
					continue
				}
			}
		} else {
			log.Infof("pagerduty: no overrides for schedule %s[%s]", schedule.Name, schedule.ID)
		}

		if len(schedule.ScheduleLayers) > 0 {
			// add rendered layers
			for _, l := range schedule.ScheduleLayers {
				for _, e := range l.RenderedScheduleEntries {
					if _, ok := uniqueUsers[e.User.ID]; !ok {
						uniqueUsers[e.User.ID] = struct{}{}
						users = append(users, c.getUser(e.User))
					}
				}
			}
		}
	}
	return users, schedules, nil
}

func (c *PdClient) getUser(user pagerduty.APIObject) pagerduty.User {
	o := pagerduty.GetUserOptions{
		Includes: []string{"contact_methods"},
	}
	u, err := c.pagerdutyClient.GetUserWithContext(context.TODO(), user.ID, o)
	if err != nil {
		return pagerduty.User{
			APIObject: user,
			Name:      user.Summary,
		}
	}
	return *u
}

// TeamMembers returns a pagerduty schedule for the given name or an error.
func (c *PdClient) TeamMembers(teamIDs []string) ([]pagerduty.User, []pagerduty.APIObject, error) {
	userListOpts := pagerduty.ListUsersOptions{}
	userListOpts.Includes = []string{"contact_methods", "notification_rules"}
	userListOpts.TeamIDs = teamIDs

	response, err := c.pagerdutyClient.ListUsersWithContext(context.TODO(), userListOpts)

	if err != nil {
		return nil, nil, err
	}

	teamObjects := []pagerduty.APIObject{}
	for _, id := range teamIDs {
		response, err := c.pagerdutyClient.GetTeamWithContext(context.TODO(), id)
		if err != nil {
			return nil, nil, fmt.Errorf("pagerduty: team not found: %w", err)
		}
		teamObjects = append(teamObjects, response.APIObject)
	}
	return response.Users, teamObjects, nil
}

// listOnCallUsers returns unique PagerDuty users for a list of OnCalls
func (c *PdClient) listOnCallUsers(onCalls []pagerduty.OnCall) (users []pagerduty.User) {
	opts := pagerduty.GetUserOptions{Includes: []string{"contact_methods"}}

	distinctUsers := make(map[string]struct{})
	for _, u := range onCalls {
		if _, ok := distinctUsers[u.User.ID]; ok {
			// duplicate user
			log.Debugf("pagerduty: skipping duplicate onCall user %s", u.User.ID)
			continue
		}
		distinctUsers[u.User.ID] = struct{}{}

		user, err := c.pagerdutyClient.GetUserWithContext(context.TODO(), u.User.ID, opts)
		if err != nil {
			log.Infof("pagerduty: retrieving user '%s' failed", u.User.ID)
			users = append(users, pagerduty.User{
				APIObject: u.User.APIObject,
				Name:      u.User.Summary})
			continue
		}
		users = append(users, *user)
	}
	return users
}

// listOnCallSchedules returns actual pagerDuty schedule API objects for a list of schedule IDs
func (c *PdClient) listOnCallSchedules(ids []string, since, until offsetInHours) (schedules []pagerduty.APIObject, err error) {
	// query options for schedule and override request (we needed since the api doesn't deliver the override info, beside api docu said it should)
	scheduleOpts := pagerduty.GetScheduleOptions{
		TimeZone: "UTC",
		Since:    util.TimestampToString(time.Now().UTC().Add(-since)),
		Until:    util.TimestampToString(time.Now().UTC().Add(until)),
	}
	for _, id := range ids {
		schedule, err := c.pagerdutyClient.GetScheduleWithContext(context.TODO(), id, scheduleOpts)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule.APIObject)
	}
	return schedules, nil
}
