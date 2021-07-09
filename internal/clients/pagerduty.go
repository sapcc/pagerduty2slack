package clients

import (
    "fmt"
    log "github.com/Sirupsen/logrus"
    "github.com/ahmetb/go-linq"
    "github.com/sapcc/pulsar/pkg/util"
    "time"

    "github.com/PagerDuty/go-pagerduty"
    "github.com/pkg/errors"
    "github.com/sapcc/pagerduty2slack/internal/config"
)

// PdClient wraps the pagerduty client.
type PdClient struct {
    cfg             *config.PagerdutyConfig
    pagerdutyClient *pagerduty.Client
    apiUserInstance *pagerduty.User
}

// PdNewClient returns a new PagerdutyClient or an error.
func PdNewClient(cfg *config.PagerdutyConfig) (*PdClient, error) {
    pagerdutyClient := pagerduty.NewClient(cfg.AuthToken)
    if pagerdutyClient == nil {
        return nil, errors.New("failed to initialize pagerduty client")
    }

    c := &PdClient{
        cfg:             cfg,
        pagerdutyClient: pagerdutyClient,
    }

    defaultUser, err := c.PdGetUserByEmail(cfg.ApiUser)
    if err != nil {
        return nil, errors.Wrapf(err, "error getting default pagerduty user with email %s", cfg.ApiUser)
    }
    c.apiUserInstance = defaultUser

    return c, nil
}

// PdGetUserByEmail returns the pagerduty user for the given email or an error.
func (c *PdClient) PdGetUserByEmail(email string) (*pagerduty.User, error) {
    userList, err := c.pagerdutyClient.ListUsers(pagerduty.ListUsersOptions{Query: email})
    if err != nil {
        return nil, err
    }
    for _, user := range userList.Users {
        if user.Email == email {
            return &user, nil
        }
    }

    return nil, fmt.Errorf("user with email '%s' not found", email)
}

// PdFilterUserWithoutPhone gives all User without a phone number set
func (c *PdClient) PdFilterUserWithoutPhone(ul []pagerduty.User) []pagerduty.User {
    var ulf []pagerduty.User
    linq.From(ulf).WhereT(func(u pagerduty.User) bool{

        return linq.From(u.ContactMethods).SelectT(func(c pagerduty.ContactMethod) string {
            return c.Type
        }).Contains("phone_contact_method_reference")

    }).ToSlice(&ulf)
    return ulf
}


// PdListOnCallUsers returns the OnCall users being on shift now
func (c *PdClient) PdListOnCallUsers(scheduleIDs []string, sinceOffsetInHours time.Duration, untilOffsetInHours time.Duration, layerSyncStyle config.SyncStyle) ([]pagerduty.User, []pagerduty.APIObject, error) {
    if layerSyncStyle == config.FinalLayer {
        return c.pdListOnCallUseFinal(scheduleIDs, sinceOffsetInHours, untilOffsetInHours)
    } else {
        return c.pdListOnCallUseLayers(scheduleIDs, sinceOffsetInHours, untilOffsetInHours, layerSyncStyle)
    }
}

func (c *PdClient) pdListOnCallUseFinal (scheduleIDs []string, sinceOffsetInHours time.Duration, untilOffsetInHours time.Duration ) ([]pagerduty.User, []pagerduty.APIObject, error) {

    lo := pagerduty.ListOnCallOptions{
        ScheduleIDs: scheduleIDs,
        Since: util.TimestampToString(time.Now().Add(-sinceOffsetInHours)),
        Until: util.TimestampToString(time.Now().Add(untilOffsetInHours)),
        //Includes: []string{"users","schedules"}, // doesn't work - workaround sub request
    }
    onCallListD, err := c.pagerdutyClient.ListOnCalls(lo)
    var ul []pagerduty.User

    if err != nil {
        return nil, nil, err
    }

    var sl []pagerduty.APIObject
    // distinct list of user on shift
    linq.From(onCallListD.OnCalls).DistinctByT(
            func(oC pagerduty.OnCall) string { return oC.User.ID },
        ).SelectT(func(oC pagerduty.OnCall) pagerduty.User {

        o := pagerduty.GetUserOptions{
            Includes: []string{"contact_methods"},
        }
        u, err := c.pagerdutyClient.GetUser(oC.User.ID, o)

        if err != nil {
            sl = append(sl, oC.User.APIObject)
            return pagerduty.User{
                APIObject:         oC.User.APIObject,
                Name:              oC.User.Summary,
            }
        }
        return *u

    }).ToSlice(&ul)
    return ul, sl, nil
}

func (c *PdClient) pdListOnCallUseLayers (scheduleIDs []string, sinceOffsetInHours time.Duration, untilOffsetInHours time.Duration, layerSyncStyle config.SyncStyle) ([]pagerduty.User, []pagerduty.APIObject, error) {
    defer func() {
        if r := recover(); r != nil {
            log.Errorf("PROGRAMMER FAIL > %s", r.(error))
        }
    }()

    // distinct list of schedule metadata
    var sl []pagerduty.APIObject
    var ul []pagerduty.User

    // query options for schedule and override request (we needed since the api doesn't deliver the override info, beside api docu said it should)
    lo := pagerduty.GetScheduleOptions{
        TimeZone: "UTC",
        Since:    util.TimestampToString(time.Now().UTC().Add(-sinceOffsetInHours)),
        Until:    util.TimestampToString(time.Now().UTC().Add(untilOffsetInHours)),
    }
    loO:= pagerduty.ListOverridesOptions{
        Since:    util.TimestampToString(time.Now().UTC().Add(-sinceOffsetInHours)),
        Until:    util.TimestampToString(time.Now().UTC().Add(untilOffsetInHours)),
    }

    // get schedule objects
    for _, schedule := range scheduleIDs {
        var tul []pagerduty.User
        scheduleO, err := c.pagerdutyClient.GetSchedule(schedule, lo)
        if scheduleO == nil || err != nil {
            return nil, sl, err
        }

        sl = append(sl, scheduleO.APIObject)

        // get overrides (since we can't trust the info in schedule object, we have to request separately until API is fixed
        ors, _ := c.pagerdutyClient.ListOverrides(schedule, loO)
        if ors != nil {
            // add override layer if exist
            if len(ors.Overrides) > 0 {
                linq.From(ors.Overrides).SelectT(func(o pagerduty.Override) pagerduty.User {
                   return c.getUser(o.User)
                }).ToSlice(&tul)
                log.Debug(ors)
                ul = append(ul, tul...)
                // if exist and we do not need the other layers - jump to next schedule
                if layerSyncStyle == config.OverridesOnlyIfThere {
                    continue
                }
            }
        } else { log.Info("No Overrides for ", scheduleO) }

        if len(scheduleO.ScheduleLayers) > 0 {

            // add rendered layers
            linq.From(scheduleO.ScheduleLayers).SelectManyByT(
                func(sL pagerduty.ScheduleLayer) linq.Query { return linq.From(sL.RenderedScheduleEntries) },
                func(rse pagerduty.RenderedScheduleEntry, sL pagerduty.ScheduleLayer) pagerduty.User {
                    return c.getUser(rse.User)
                },
            ).ToSlice(&tul)
            ul = append(ul, tul...)
        }
    }

    linq.From(ul).DistinctByT(func (u pagerduty.User) string {return u.ID}).ToSlice(&ul)

    return ul, sl, nil
}

func (c *PdClient) getUser(user pagerduty.APIObject) pagerduty.User {
    o := pagerduty.GetUserOptions{
        Includes: []string{"contact_methods"},
    }
    u, err := c.pagerdutyClient.GetUser(user.ID, o)
    if err != nil {
        return pagerduty.User{
            APIObject: user,
            Name:      user.Summary,
        }
    }
    return *u
}

// PdGetTeamMembers returns a pagerduty schedule for the given name or an error.
func (c *PdClient) PdGetTeamMembers(teamIds []string) ([]pagerduty.User, []pagerduty.APIObject, error) {
    userListOpts := pagerduty.ListUsersOptions{}
    userListOpts.Includes = []string{"contact_methods","notification_rules"}
    userListOpts.TeamIDs = teamIds

    response, err := c.pagerdutyClient.ListUsers(userListOpts)

    if err != nil {
        return nil, nil,err
    }

    var tOL []pagerduty.APIObject
    linq.From(teamIds).SelectT(func (t string) pagerduty.APIObject {
        response, err := c.pagerdutyClient.GetTeam(t)
        if err != nil {
            return pagerduty.APIObject{}
        }
        return response.APIObject
    }).ToSlice(&tOL)
    //linq.From(response.Users).DistinctByT(func(u pagerduty.User) string {
    //    return u.ID
    //}).SelectManyByT(
    //    func (u pagerduty.User) linq.Query { return linq.From(u.Teams) },
    //    func (t pagerduty.Team, u pagerduty.User) pagerduty.APIObject { return t.APIObject },
    //    ).DistinctByT(func(t pagerduty.APIObject) string { return t.ID}).ToSlice(&tOL)

    return response.Users, tOL, nil
}