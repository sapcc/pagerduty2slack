package pagerduty

import (
	"context"
	"fmt"
	"time"

	pd "github.com/PagerDuty/go-pagerduty"
	"github.com/sapcc/pulsar/pkg/util"
	log "github.com/sirupsen/logrus"

	"github.com/sapcc/pagerduty2slack/internal/config"
)

type offsetInHours = time.Duration

// Client wraps the pagerduty client.
type Client struct {
	cfg             *config.PagerdutyConfig
	api             *pd.Client
	apiUserInstance *pd.User
}

// NewClient returns a new PagerdutyClient or an error.
func NewClient(cfg *config.PagerdutyConfig) (*Client, error) {
	pagerdutyClient := pd.NewClient(cfg.AuthToken)
	if pagerdutyClient == nil {
		return nil, fmt.Errorf("pagerduty: failed to initialize client")
	}

	c := &Client{
		cfg: cfg,
		api: pagerdutyClient,
	}

	defaultUser, err := c.findUserByEmail(cfg.APIUser)
	if err != nil {
		return nil, fmt.Errorf("pagerduty: getting default user by email '%s' failed: %w", cfg.APIUser, err)
	}
	c.apiUserInstance = defaultUser

	return c, nil
}

// findUserByEmail returns the pagerduty user for the given email or an error.
func (c *Client) findUserByEmail(email string) (*pd.User, error) {
	userList, err := c.api.ListUsersWithContext(context.TODO(), pd.ListUsersOptions{Query: email})
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

// WithoutPhone returns all users without phone number set
func (c *Client) WithoutPhone(users []pd.User) []pd.User {
	noPhoneUsers := []pd.User{}
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
func (c *Client) ListOnCallUsers(scheduleIDs []string, since, until offsetInHours, layerSyncStyle config.SyncStyle) ([]pd.User, []pd.APIObject, error) {
	if layerSyncStyle == config.FinalLayer {
		return c.listOnCallsFinalLayer(scheduleIDs, since, until)
	} else {
		return c.listOnCallsLayers(scheduleIDs, since, until, layerSyncStyle)
	}
}

func (c *Client) listOnCallsFinalLayer(scheduleIDs []string, since, until offsetInHours) (users []pd.User, schedules []pd.APIObject, err error) {
	onCallOpts := pd.ListOnCallOptions{
		ScheduleIDs: scheduleIDs,
		TimeZone:    "UTC",
		Since:       util.TimestampToString(time.Now().UTC().Add(-since)),
		Until:       util.TimestampToString(time.Now().UTC().Add(until)),
		//Includes: []string{"users","schedules"}, // doesn't work - workaround sub request
	}
	resp, err := c.api.ListOnCallsWithContext(context.TODO(), onCallOpts)
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

func (c *Client) listOnCallsLayers(scheduleIDs []string, since, until offsetInHours, layerSyncStyle config.SyncStyle) (users []pd.User, schedules []pd.APIObject,
	err error) {
	// query options for schedule and override request (we needed since the api doesn't deliver the override info, beside api docu said it should)
	scheduleOpts := pd.GetScheduleOptions{
		TimeZone: "UTC",
		Since:    util.TimestampToString(time.Now().UTC().Add(-since)),
		Until:    util.TimestampToString(time.Now().UTC().Add(until)),
	}
	overrideOpts := pd.ListOverridesOptions{
		Since: util.TimestampToString(time.Now().UTC().Add(-since)),
		Until: util.TimestampToString(time.Now().UTC().Add(until)),
	}

	uniqueUsers := make(map[string]struct{})
	// get schedule objects
	for _, id := range scheduleIDs {
		schedule, err := c.api.GetScheduleWithContext(context.TODO(), id, scheduleOpts)
		if schedule == nil || err != nil {
			return nil, schedules, err
		}
		schedules = append(schedules, schedule.APIObject)

		// get overrides (since we can't trust the info in schedule object, we have to request separately until API is fixed
		overrides, err := c.api.ListOverridesWithContext(context.TODO(), id, overrideOpts)
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
				log.Debugf("pagerduty: handled overrides for schedule %s[%s]", schedule.Name, schedule.ID)
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

// getUser
func (c *Client) getUser(user pd.APIObject) pd.User {
	o := pd.GetUserOptions{
		Includes: []string{"contact_methods"},
	}
	u, err := c.api.GetUserWithContext(context.TODO(), user.ID, o)
	if err != nil {
		return pd.User{
			APIObject: user,
			Name:      user.Summary,
		}
	}
	return *u
}

// TeamMembers returns a pagerduty schedule for the given name or an error.
func (c *Client) TeamMembers(teamIDs []string) ([]pd.User, []pd.APIObject, error) {
	userListOpts := pd.ListUsersOptions{}
	userListOpts.Includes = []string{"contact_methods", "notification_rules"}
	userListOpts.TeamIDs = teamIDs

	response, err := c.api.ListUsersWithContext(context.TODO(), userListOpts)

	if err != nil {
		return nil, nil, err
	}

	teamObjects := []pd.APIObject{}
	for _, id := range teamIDs {
		response, err := c.api.GetTeamWithContext(context.TODO(), id)
		if err != nil {
			return nil, nil, fmt.Errorf("pagerduty: team not found: %w", err)
		}
		teamObjects = append(teamObjects, response.APIObject)
	}
	return response.Users, teamObjects, nil
}

// listOnCallUsers returns unique PagerDuty users for a list of OnCalls
func (c *Client) listOnCallUsers(onCalls []pd.OnCall) (users []pd.User) {
	opts := pd.GetUserOptions{Includes: []string{"contact_methods"}}

	distinctUsers := make(map[string]struct{})
	for _, u := range onCalls {
		if _, ok := distinctUsers[u.User.ID]; ok {
			// duplicate user
			log.Debugf("pagerduty: skipping duplicate onCall user %s", u.User.ID)
			continue
		}
		distinctUsers[u.User.ID] = struct{}{}

		user, err := c.api.GetUserWithContext(context.TODO(), u.User.ID, opts)
		if err != nil {
			log.Infof("pagerduty: retrieving user '%s' failed", u.User.ID)
			users = append(users, pd.User{
				APIObject: u.User.APIObject,
				Name:      u.User.Summary})
			continue
		}
		users = append(users, *user)
	}
	return users
}

// listOnCallSchedules returns actual pagerDuty schedule API objects for a list of schedule IDs
func (c *Client) listOnCallSchedules(ids []string, since, until offsetInHours) (schedules []pd.APIObject, err error) {
	// query options for schedule and override request (we needed since the api doesn't deliver the override info, beside api docu said it should)
	scheduleOpts := pd.GetScheduleOptions{
		TimeZone: "UTC",
		Since:    util.TimestampToString(time.Now().UTC().Add(-since)),
		Until:    util.TimestampToString(time.Now().UTC().Add(until)),
	}
	for _, id := range ids {
		schedule, err := c.api.GetScheduleWithContext(context.TODO(), id, scheduleOpts)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule.APIObject)
	}
	return schedules, nil
}
